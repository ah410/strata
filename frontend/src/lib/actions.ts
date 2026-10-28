import type { Entry } from "./api";

/**
 * Every file action, described once. The toolbar and context menu render
 * from this list; App supplies what each id does.
 */
export type ActionId =
  | "open" | "extract" | "rename" | "history" | "star" | "cut" | "clip" | "pasteInto" | "move" | "copy"
  | "download" | "compress" | "delete"
  | "paste" | "newFolder" | "newFile" | "uploadFolder" | "upload" | "layout" | "selectAll" | "refresh";

export interface Ctx {
  /** What the action applies to; empty for the folder background. */
  selection: Entry[];
  /** Favorites need a config volume. */
  canStar: boolean;
  favorites: Set<string>;
  layout: "list" | "grid";
  /** Items waiting on the clipboard. */
  clipboard: number;
}

export interface Action {
  id: ActionId;
  label: (c: Ctx) => string;
  /** Item actions need a selection; space actions act on the open folder. */
  on: "item" | "space";
  /** Menus draw a separator between groups. */
  group: number;
  toolbar?: boolean;
  /** How the shortcut list names it, where the label depends on context. */
  name?: string;
  /** Shortcuts, as combo() names them; the first is shown in menus. */
  keys?: string[];
  primary?: boolean;
  danger?: boolean;
  when?: (c: Ctx) => boolean;
}

const one = (c: Ctx) => c.selection.length === 1;
const oneFile = (c: Ctx) => one(c) && !c.selection[0].isDir;
/** Archive formats the server can unpack. */
export const EXTRACTABLE = /\.(zip|tar|tar\.gz|tgz)$/i;
const count = (n: number) => `${n} item${n === 1 ? "" : "s"}`;
const allStarred = (c: Ctx) => c.selection.length > 0 && c.selection.every((e) => c.favorites.has(e.path));

export const ACTIONS: Action[] = [
  { id: "open", label: () => "Open", on: "item", group: 0, when: one, keys: ["Enter"] },
  { id: "extract", label: () => "Extract here", on: "item", group: 0, toolbar: true, when: (c) => oneFile(c) && EXTRACTABLE.test(c.selection[0].name) },
  { id: "rename", label: () => "Rename", on: "item", group: 1, toolbar: true, when: one, keys: ["F2"] },
  { id: "history", label: () => "History", on: "item", group: 1, toolbar: true, when: oneFile },
  { id: "star", label: (c) => (allStarred(c) ? "Unstar" : "Star"), on: "item", group: 1, toolbar: true, when: (c) => c.canStar },
  { id: "cut", label: () => "Cut", on: "item", group: 2, keys: ["Mod+X"] },
  { id: "clip", label: () => "Copy", on: "item", group: 2, keys: ["Mod+C"] },
  { id: "pasteInto", label: (c) => `Paste ${count(c.clipboard)} into folder`, on: "item", group: 2, when: (c) => c.clipboard > 0 && one(c) && c.selection[0].isDir },
  { id: "move", label: () => "Move to…", on: "item", group: 2, toolbar: true },
  { id: "copy", label: () => "Copy to…", on: "item", group: 2, toolbar: true },
  { id: "download", label: () => "Download", on: "item", group: 3, toolbar: true },
  { id: "compress", label: () => "Compress to zip", on: "item", group: 3 },
  { id: "delete", label: () => "Delete", on: "item", group: 4, toolbar: true, danger: true, keys: ["Delete", "Mod+Backspace"] },

  { id: "paste", label: (c) => `Paste ${count(c.clipboard)}`, on: "space", group: 0, when: (c) => c.clipboard > 0, keys: ["Mod+V"], name: "Paste" },
  { id: "layout", label: (c) => (c.layout === "list" ? "Show as grid" : "Show as list"), on: "space", group: 0, toolbar: true, keys: ["Alt+V"], name: "Switch between list and grid" },
  { id: "newFolder", label: () => "New folder", on: "space", group: 1, toolbar: true, keys: ["Alt+N"] },
  { id: "newFile", label: () => "New file", on: "space", group: 1, toolbar: true },
  { id: "uploadFolder", label: () => "Upload folder", on: "space", group: 2, toolbar: true },
  { id: "upload", label: () => "Upload", on: "space", group: 2, toolbar: true, primary: true, keys: ["Alt+U"] },
  { id: "selectAll", label: () => "Select all", on: "space", group: 3, keys: ["Mod+A"] },
  { id: "refresh", label: () => "Refresh", on: "space", group: 3 },
];

/** The actions that apply in c, in display order. */
export function actionsFor(c: Ctx, where: "toolbar" | "menu"): Action[] {
  const on = c.selection.length ? "item" : "space";
  return ACTIONS.filter((a) => a.on === on && (where === "menu" || a.toolbar) && (!a.when || a.when(c)));
}

/**
 * The action a key press runs. Folder actions like paste or new folder
 * work whatever is selected; item actions need a selection.
 */
export function actionForKey(key: string, c: Ctx): Action | undefined {
  const space = { ...c, selection: [] };
  return ACTIONS.find((a) => {
    if (!a.keys?.includes(key)) return false;
    const at = a.on === "space" ? space : c;
    return (a.on === "space" || c.selection.length > 0) && (!a.when || a.when(at));
  });
}
