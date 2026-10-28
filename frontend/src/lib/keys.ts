/** Key handling shared by the shortcut handler, menus and the Settings list. */

export const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

interface KeyLike { key: string; code?: string; ctrlKey: boolean; metaKey: boolean; altKey: boolean; shiftKey: boolean }

/**
 * A normalized name for a key press, e.g. "Mod+C", "Shift+ArrowDown",
 * "Alt+N", "?". Mod is Cmd on a Mac and Ctrl elsewhere. Letters come from
 * the physical key when a modifier is held, since Option on a Mac turns
 * Option+N into "˜".
 */
export function combo(e: KeyLike, mac = isMac): string {
  const mod = mac ? e.metaKey : e.ctrlKey;
  let key = e.key;
  if (e.code?.startsWith("Key") && (mod || e.altKey)) key = e.code.slice(3);
  else if (e.code?.startsWith("Digit") && (mod || e.altKey)) key = e.code.slice(5);
  else if (key.length === 1) key = key.toUpperCase();
  if (key === " ") key = "Space";
  const printable = key.length === 1 && !mod && !e.altKey;
  const parts = [];
  if (mod) parts.push("Mod");
  if (e.altKey) parts.push("Alt");
  // Shift is part of a printable character already ("?" not "Shift+/").
  if (e.shiftKey && !printable) parts.push("Shift");
  parts.push(key);
  return parts.join("+");
}

const SYMBOL: Record<string, string> = {
  ArrowUp: "↑", ArrowDown: "↓", ArrowLeft: "←", ArrowRight: "→",
  Backspace: "⌫", Enter: "↵", Escape: "Esc", Delete: "Del", Space: "Space",
};

/** How a combo is printed for this platform: "⌘C" on a Mac, "Ctrl+C" elsewhere. */
export function display(c: string, mac = isMac): string {
  const parts = c.split("+").map((p) => {
    if (p === "Mod") return mac ? "⌘" : "Ctrl";
    if (p === "Alt") return mac ? "⌥" : "Alt";
    if (p === "Shift") return mac ? "⇧" : "Shift";
    if (!mac && (p === "Enter" || p === "Backspace")) return p;
    return SYMBOL[p] ?? p;
  });
  return parts.join(mac ? "" : "+");
}

/** Shortcuts that move around rather than run an action. */
export const NAV_KEYS: { keys: string[]; label: string }[] = [
  { keys: ["ArrowUp", "ArrowDown"], label: "Move between items (←→ too in grid view)" },
  { keys: ["Shift+ArrowDown"], label: "Extend the selection" },
  { keys: ["Space"], label: "Select or unselect the current item" },
  { keys: ["Home", "End"], label: "First or last item" },
  { keys: ["Backspace", "Alt+ArrowUp"], label: "Go to the parent folder" },
  { keys: ["Mod+K"], label: "Go to anything: actions, files and folders" },
  { keys: ["/"], label: "Search this folder" },
  { keys: ["Escape"], label: "Clear the selection or search" },
  { keys: ["Shift+F10"], label: "Open the menu for the selection" },
  { keys: ["?"], label: "Show these shortcuts" },
];

/** Typing a name jumps to it; keystrokes within this gap join one prefix. */
export const TYPEAHEAD_MS = 800;

/** The first name starting with prefix (case-insensitive), searching from start and wrapping. */
export function typeahead(names: string[], prefix: string, start = 0): number {
  const p = prefix.toLowerCase();
  for (let k = 0; k < names.length; k++) {
    const i = (start + k) % names.length;
    if (names[i].toLowerCase().startsWith(p)) return i;
  }
  return -1;
}

/** Index after moving by key in a list or a grid with cols columns, clamped. */
export function step(i: number, key: string, n: number, cols: number): number {
  const d: Record<string, number> = { ArrowUp: -cols, ArrowDown: cols, ArrowLeft: cols > 1 ? -1 : 0, ArrowRight: cols > 1 ? 1 : 0 };
  if (key === "Home") return 0;
  if (key === "End") return n - 1;
  if (i < 0) return 0;
  return Math.max(0, Math.min(n - 1, i + (d[key] ?? 0)));
}
