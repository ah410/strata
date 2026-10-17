import { useSyncExternalStore } from "react";

/**
 * Themes are plain palettes turned into the CSS variables styles.css reads.
 * File-kind colours come from each palette's accents so every theme keeps
 * the "colour means kind" rule.
 */

interface Palette {
  label: string;
  dark: boolean;
  bg: string;
  surface: string;
  raised: string;
  line: string;
  lineSoft: string;
  ink: string;
  inkDim: string;
  inkFaint: string;
  select: string;
  yellow: string;
  green: string;
  purple: string;
  red: string;
  blue: string;
  cyan: string;
  orange: string;
  pink: string;
  brown: string;
  crit?: string;
}

export const THEMES = {
  "strata-dark": {
    label: "Strata Dark", dark: true,
    bg: "#16181A", surface: "#1C1F21", raised: "#23272A", line: "#2C3135", lineSoft: "#23272A",
    ink: "#E6E7E4", inkDim: "#9AA09C", inkFaint: "#6B716D", select: "#7FB2E5",
    yellow: "#D8C07A", green: "#6FBF8E", purple: "#B99BD4", red: "#D98A80", blue: "#7FA8E5",
    cyan: "#7FB2E5", orange: "#E0A050", pink: "#E0A86B", brown: "#A8906B", crit: "#D96E62",
  },
  "strata-light": {
    label: "Strata Light", dark: false,
    bg: "#F7F7F5", surface: "#FFFFFF", raised: "#F0F0ED", line: "#DEDEDA", lineSoft: "#EAEAE6",
    ink: "#1A1C1B", inkDim: "#5E635F", inkFaint: "#8A908B", select: "#2C6FAF",
    yellow: "#A8842C", green: "#2E7D4F", purple: "#7A52A3", red: "#B6483C", blue: "#35609E",
    cyan: "#2C6FAF", orange: "#A06218", pink: "#A86A1E", brown: "#7A6534",
  },
  paper: {
    label: "Spreadsheet", dark: false,
    bg: "#FFFFFF", surface: "#FFFFFF", raised: "#F3F3F3", line: "#D4D4D4", lineSoft: "#E1E1E1",
    ink: "#1F1F1F", inkDim: "#555555", inkFaint: "#8A8A8A", select: "#217346",
    yellow: "#A8842C", green: "#217346", purple: "#7A52A3", red: "#B6483C", blue: "#35609E",
    cyan: "#2C6FAF", orange: "#A06218", pink: "#A86A1E", brown: "#7A6534",
  },
  "solarized-light": {
    label: "Solarized Light", dark: false,
    bg: "#FDF6E3", surface: "#EEE8D5", raised: "#E6DFCA", line: "#D6CFB9", lineSoft: "#EAE3CE",
    ink: "#073642", inkDim: "#586E75", inkFaint: "#93A1A1", select: "#268BD2",
    yellow: "#B58900", green: "#859900", purple: "#6C71C4", red: "#DC322F", blue: "#268BD2",
    cyan: "#2AA198", orange: "#CB4B16", pink: "#D33682", brown: "#8A6D0B",
  },
  "solarized-dark": {
    label: "Solarized Dark", dark: true,
    bg: "#002B36", surface: "#073642", raised: "#0B4252", line: "#17505E", lineSoft: "#0A3A47",
    ink: "#EEE8D5", inkDim: "#93A1A1", inkFaint: "#657B83", select: "#268BD2",
    yellow: "#B58900", green: "#859900", purple: "#6C71C4", red: "#DC322F", blue: "#268BD2",
    cyan: "#2AA198", orange: "#CB4B16", pink: "#D33682", brown: "#A08A4E",
  },
  nord: {
    label: "Nord", dark: true,
    bg: "#2E3440", surface: "#3B4252", raised: "#434C5E", line: "#4C566A", lineSoft: "#3B4252",
    ink: "#ECEFF4", inkDim: "#D8DEE9", inkFaint: "#9AA5B8", select: "#88C0D0",
    yellow: "#EBCB8B", green: "#A3BE8C", purple: "#B48EAD", red: "#BF616A", blue: "#81A1C1",
    cyan: "#8FBCBB", orange: "#D08770", pink: "#B48EAD", brown: "#A39074",
  },
  dracula: {
    label: "Dracula", dark: true,
    bg: "#282A36", surface: "#21222C", raised: "#343746", line: "#44475A", lineSoft: "#323443",
    ink: "#F8F8F2", inkDim: "#C6C8D1", inkFaint: "#6272A4", select: "#BD93F9",
    yellow: "#F1FA8C", green: "#50FA7B", purple: "#BD93F9", red: "#FF5555", blue: "#8BE9FD",
    cyan: "#8BE9FD", orange: "#FFB86C", pink: "#FF79C6", brown: "#C9A26B",
  },
  gruvbox: {
    label: "Gruvbox", dark: true,
    bg: "#282828", surface: "#32302F", raised: "#3C3836", line: "#504945", lineSoft: "#3C3836",
    ink: "#EBDBB2", inkDim: "#BDAE93", inkFaint: "#928374", select: "#83A598",
    yellow: "#FABD2F", green: "#B8BB26", purple: "#D3869B", red: "#FB4934", blue: "#83A598",
    cyan: "#8EC07C", orange: "#FE8019", pink: "#D3869B", brown: "#D79921",
  },
  "catppuccin-latte": {
    label: "Catppuccin Latte", dark: false,
    bg: "#EFF1F5", surface: "#E6E9EF", raised: "#DCE0E8", line: "#CCD0DA", lineSoft: "#DCE0E8",
    ink: "#4C4F69", inkDim: "#5C5F77", inkFaint: "#8C8FA1", select: "#1E66F5",
    yellow: "#DF8E1D", green: "#40A02B", purple: "#8839EF", red: "#D20F39", blue: "#1E66F5",
    cyan: "#04A5E5", orange: "#FE640B", pink: "#EA76CB", brown: "#DD7878",
  },
  "catppuccin-mocha": {
    label: "Catppuccin Mocha", dark: true,
    bg: "#1E1E2E", surface: "#181825", raised: "#313244", line: "#45475A", lineSoft: "#313244",
    ink: "#CDD6F4", inkDim: "#BAC2DE", inkFaint: "#7F849C", select: "#89B4FA",
    yellow: "#F9E2AF", green: "#A6E3A1", purple: "#CBA6F7", red: "#F38BA8", blue: "#89B4FA",
    cyan: "#89DCEB", orange: "#FAB387", pink: "#F5C2E7", brown: "#F2CDCD",
  },
  "tokyo-night": {
    label: "Tokyo Night", dark: true,
    bg: "#1A1B26", surface: "#16161E", raised: "#24283B", line: "#2F3549", lineSoft: "#232433",
    ink: "#C0CAF5", inkDim: "#A9B1D6", inkFaint: "#565F89", select: "#7AA2F7",
    yellow: "#E0AF68", green: "#9ECE6A", purple: "#BB9AF7", red: "#F7768E", blue: "#7AA2F7",
    cyan: "#7DCFFF", orange: "#FF9E64", pink: "#BB9AF7", brown: "#A08A5C",
  },
} satisfies Record<string, Palette>;

export type ThemeId = keyof typeof THEMES;
export const THEME_IDS = Object.keys(THEMES) as ThemeId[];

/** Viewers that can carry their own theme, independent of the app. */
export const SCOPES = {
  sheet: "Spreadsheets",
  text: "Text & CSV",
  media: "Images, video & PDF",
} as const;
export type Scope = keyof typeof SCOPES;

export interface Appearance {
  app: ThemeId | "system";
  systemLight: ThemeId;
  systemDark: ThemeId;
  scopes: Partial<Record<Scope, ThemeId>>; // missing = same as app
}

const DEFAULTS: Appearance = {
  app: "system",
  systemLight: "strata-light",
  systemDark: "strata-dark",
  // Workbook colours assume a white page, so sheets start on paper.
  scopes: { sheet: "paper" },
};

const KEY = "strata.appearance";
const VARS_KEY = "strata.vars"; // read by the pre-paint script in index.html

function load(): Appearance {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) ?? "null");
    if (!raw) return DEFAULTS;
    const ok = (t: unknown): t is ThemeId => typeof t === "string" && t in THEMES;
    const scopes: Appearance["scopes"] = {};
    for (const s of Object.keys(SCOPES) as Scope[]) {
      if (ok(raw.scopes?.[s])) scopes[s] = raw.scopes[s];
    }
    return {
      app: raw.app === "system" || ok(raw.app) ? raw.app : DEFAULTS.app,
      systemLight: ok(raw.systemLight) ? raw.systemLight : DEFAULTS.systemLight,
      systemDark: ok(raw.systemDark) ? raw.systemDark : DEFAULTS.systemDark,
      scopes: raw.scopes ? scopes : DEFAULTS.scopes,
    };
  } catch {
    return DEFAULTS;
  }
}

const media = window.matchMedia("(prefers-color-scheme: dark)");
let state = load();
let snapshot = { appearance: state, systemDark: media.matches };
const listeners = new Set<() => void>();

function emit() {
  snapshot = { appearance: state, systemDark: media.matches };
  applyAppTheme();
  listeners.forEach((l) => l());
}

media.addEventListener("change", emit);

export function setAppearance(patch: Partial<Appearance>) {
  state = { ...state, ...patch };
  try {
    localStorage.setItem(KEY, JSON.stringify(state));
  } catch {
    // Private windows can refuse storage; the choice still applies this session.
  }
  emit();
}

export function setScopeTheme(scope: Scope, theme: ThemeId | null) {
  const scopes = { ...state.scopes };
  if (theme) scopes[scope] = theme;
  else delete scopes[scope];
  setAppearance({ scopes });
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function useAppearance() {
  return useSyncExternalStore(subscribe, () => snapshot);
}

export function appThemeId(): ThemeId {
  const { appearance: a, systemDark } = snapshot;
  if (a.app !== "system") return a.app;
  return systemDark ? a.systemDark : a.systemLight;
}

/** The theme a viewer should use: its own override, or the app's. */
export function useScopeTheme(scope: Scope): ThemeId {
  const { appearance } = useAppearance();
  return appearance.scopes[scope] ?? appThemeId();
}

export function isDark(id: ThemeId): boolean {
  return THEMES[id].dark;
}

/** CSS custom properties for a theme, usable as an inline style object. */
export function themeVars(id: ThemeId): Record<string, string> {
  const p: Palette = THEMES[id];
  return {
    "--bg": p.bg,
    "--surface": p.surface,
    "--raised": p.raised,
    "--line": p.line,
    "--line-soft": p.lineSoft,
    "--ink": p.ink,
    "--ink-dim": p.inkDim,
    "--ink-faint": p.inkFaint,
    "--select": p.select,
    "--select-bg": withAlpha(p.select, p.dark ? 0.14 : 0.1),
    "--on-select": readableOn(p.select),
    "--k-folder": p.yellow,
    "--k-sheet": p.green,
    "--k-image": p.purple,
    "--k-pdf": p.red,
    "--k-video": p.cyan,
    "--k-audio": p.pink,
    "--k-text": p.inkDim,
    "--k-archive": p.brown,
    "--k-doc": p.blue,
    "--k-slides": p.orange,
    "--k-file": p.inkFaint,
    "--ok": p.green,
    "--warn": p.orange,
    "--crit": p.crit ?? p.red,
    "color-scheme": p.dark ? "dark" : "light",
  };
}

function applyAppTheme() {
  const id = appThemeId();
  const root = document.documentElement;
  const vars = themeVars(id);
  for (const [k, v] of Object.entries(vars)) root.style.setProperty(k, v);
  root.dataset.theme = id;
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", THEMES[id].bg);
  try {
    localStorage.setItem(VARS_KEY, root.style.cssText);
  } catch {
    // Only costs a flash of the default palette on next load.
  }
}

applyAppTheme();

/** Relative luminance (WCAG), 0 = black, 1 = white. */
export function luminance(hex: string): number {
  const n = parseInt(hex.slice(1), 16);
  const ch = [n >> 16, (n >> 8) & 255, n & 255].map((v) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * ch[0] + 0.7152 * ch[1] + 0.0722 * ch[2];
}

/** Black or white, whichever has the higher contrast ratio against bg. */
export function readableOn(bg: string): string {
  const l = luminance(bg);
  return (l + 0.05) / 0.05 > 1.05 / (l + 0.05) ? "#111111" : "#FFFFFF";
}

function withAlpha(hex: string, a: number): string {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${n >> 16}, ${(n >> 8) & 255}, ${n & 255}, ${a})`;
}
