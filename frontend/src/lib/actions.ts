import type { Entry } from "./api";

/**
 * Every file action, described once. The toolbar and context menu render
 * from this list; App supplies what each id does.
 */
export type ActionId =
  | "open" | "extract" | "rename" | "history" | "star" | "move" | "copy" | "download" | "delete"
  | "newFolder" | "newFile" | "uploadFolder" | "upload" | "layout" | "selectAll" | "refresh";

export interface Ctx {
  /** What the action applies to; empty for the folder background. */
  selection: Entry[];
  /** Favorites need a config volume. */
  canStar: boolean;
  favorites: Set<string>;
  layout: "list" | "grid";
}

export interface Action {
  id: ActionId;
  label: (c: Ctx) => string;
  /** Item actions need a selection; space actions act on the open folder. */
  on: "item" | "space";
  /** Menus draw a separator between groups. */
  group: number;
  toolbar?: boolean;
  primary?: boolean;
  danger?: boolean;
  when?: (c: Ctx) => boolean;
}

const one = (c: Ctx) => c.selection.length === 1;
const oneFile = (c: Ctx) => one(c) && !c.selection[0].isDir;
/** Archive formats the server can unpack. */
export const EXTRACTABLE = /\.(zip|tar|tar\.gz|tgz)$/i;
const allStarred = (c: Ctx) => c.selection.length > 0 && c.selection.every((e) => c.favorites.has(e.path));

export const ACTIONS: Action[] = [
  { id: "open", label: () => "Open", on: "item", group: 0, when: one },
  { id: "extract", label: () => "Extract here", on: "item", group: 0, toolbar: true, when: (c) => oneFile(c) && EXTRACTABLE.test(c.selection[0].name) },
  { id: "rename", label: () => "Rename", on: "item", group: 1, toolbar: true, when: one },
  { id: "history", label: () => "History", on: "item", group: 1, toolbar: true, when: oneFile },
  { id: "star", label: (c) => (allStarred(c) ? "Unstar" : "Star"), on: "item", group: 1, toolbar: true, when: (c) => c.canStar },
  { id: "move", label: () => "Move", on: "item", group: 2, toolbar: true },
  { id: "copy", label: () => "Copy", on: "item", group: 2, toolbar: true },
  { id: "download", label: () => "Download", on: "item", group: 3, toolbar: true },
  { id: "delete", label: () => "Delete", on: "item", group: 4, toolbar: true, danger: true },

  { id: "layout", label: (c) => (c.layout === "list" ? "Show as grid" : "Show as list"), on: "space", group: 0, toolbar: true },
  { id: "newFolder", label: () => "New folder", on: "space", group: 1, toolbar: true },
  { id: "newFile", label: () => "New file", on: "space", group: 1, toolbar: true },
  { id: "uploadFolder", label: () => "Upload folder", on: "space", group: 2, toolbar: true },
  { id: "upload", label: () => "Upload", on: "space", group: 2, toolbar: true, primary: true },
  { id: "selectAll", label: () => "Select all", on: "space", group: 3 },
  { id: "refresh", label: () => "Refresh", on: "space", group: 3 },
];

/** The actions that apply in c, in display order. */
export function actionsFor(c: Ctx, where: "toolbar" | "menu"): Action[] {
  const on = c.selection.length ? "item" : "space";
  return ACTIONS.filter((a) => a.on === on && (where === "menu" || a.toolbar) && (!a.when || a.when(c)));
}
