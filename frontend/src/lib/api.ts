export type Kind =
  | "folder" | "sheet" | "csv" | "image" | "pdf" | "video"
  | "audio" | "text" | "archive" | "doc" | "slides" | "file";

export interface Entry {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modified: string;
  kind: Kind;
}

export interface Listing {
  path: string;
  entries: Entry[];
}

export interface TrashItem {
  id: string;
  path: string;
  deleted: string;
  size: number;
  isDir: boolean;
}

export interface Version {
  id: string;
  time: string;
  size: number;
}

export interface SearchResult {
  entries: Entry[];
  truncated: boolean;
}

export interface Usage {
  total: number;
  free: number;
  used: number;
}

export interface CellStyle {
  bold?: boolean;
  italic?: boolean;
  size?: number;
  color?: string;
  fill?: string;
  align?: string;
  family?: string;
  wrap?: boolean;
  numFmt?: string;
}

export interface Cell {
  ref: string;
  col: number;
  row: number;
  value: string;
  formula?: string;
  style?: CellStyle;
}

export interface Merge {
  ref: string;
  row: number;
  col: number;
  rowSpan: number;
  colSpan: number;
}

export interface Grid {
  sheets: string[];
  active: string;
  cells: Cell[];
  merges: Merge[] | null;
  columns: { index: number; width: number }[];
  rows: { index: number; height: number }[];
  maxRow: number;
  maxCol: number;
  truncated: boolean;
  version: string;
}

/** An HTTP error that keeps its status, so callers can react to e.g. 409. */
export class ApiError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
  }
}

// The server rejects state-changing requests without this header; a
// cross-site page can't add it without a CORS preflight we never approve.
const GUARD = { "X-Strata": "1" };

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { ...init, headers: { ...GUARD, ...init?.headers } });
  if ((res.status === 401 || res.status === 428) && !url.startsWith("/api/session") && !url.startsWith("/api/login") && !url.startsWith("/api/setup")) {
    signedOut();
  }
  if (!res.ok) {
    throw new ApiError((await res.text()).trim() || `${res.status} ${res.statusText}`, res.status);
  }
  return res.json() as Promise<T>;
}

const q = (p: string) => encodeURIComponent(p);

/** Fired when the server says the session is gone, so the UI can ask again. */
export const SIGNED_OUT = "strata:signed-out";
export const signedOut = () => window.dispatchEvent(new Event(SIGNED_OUT));

export const api = {
  /** Resolves to whether sign-in is enabled; rejects with 401 when signed out. */
  session: () => req<{ ok: true; auth?: boolean }>("/api/session").then((s) => s.auth !== false),

  login: (passphrase: string) =>
    req<{ ok: true }>("/api/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ passphrase }),
    }),

  logout: () => req<{ ok: true }>("/api/logout", { method: "POST" }),

  setup: (code: string, passphrase: string) =>
    req<{ ok: true }>("/api/setup", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code, passphrase }),
    }),

  list: (path: string) => req<Listing>(`/api/list?path=${q(path)}`),
  usage: () => req<Usage>("/api/usage"),
  text: (path: string) => req<{ content: string; truncated: boolean }>(`/api/text?path=${q(path)}`),

  sheet: (path: string, sheet?: string) =>
    req<Grid>(`/api/sheet?path=${q(path)}${sheet ? `&sheet=${q(sheet)}` : ""}`),

  saveSheet: (path: string, sheet: string, version: string, edits: { ref: string; value: string }[]) =>
    req<Grid>("/api/sheet", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path, sheet, version, edits }),
    }),

  mkdir: (path: string) =>
    req<{ ok: true }>("/api/mkdir", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path }),
    }),

  move: (from: string, to: string) =>
    req<{ ok: true }>("/api/move", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ from, to }),
    }),

  copy: (from: string, to: string) =>
    req<{ ok: true }>("/api/copy", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ from, to }),
    }),

  /** Moves to the trash; `permanent` only when a location has no trash. */
  remove: (path: string, permanent = false) =>
    req<{ ok: true; trashId?: string }>("/api/delete", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path, permanent }),
    }),

  trash: () => req<{ items: TrashItem[] }>("/api/trash").then((r) => r.items),

  restore: (id: string) =>
    req<{ path: string }>("/api/trash/restore", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id }),
    }),

  purge: (id: string | "all") =>
    req<{ ok: true }>("/api/trash/purge", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(id === "all" ? { all: true } : { id }),
    }),

  versions: (path: string) => req<{ versions: Version[] }>(`/api/versions?path=${q(path)}`).then((r) => r.versions),

  restoreVersion: (path: string, id: string) =>
    req<{ ok: true }>("/api/versions/restore", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path, id }),
    }),

  versionUrl: (path: string, id: string) => `/api/versions/download?path=${q(path)}&id=${q(id)}`,

  search: (path: string, query: string, signal?: AbortSignal) =>
    req<SearchResult>(`/api/search?path=${q(path)}&q=${q(query)}`, { signal }),

  zipUrl: (paths: string[]) => `/api/zip?${paths.map((p) => `path=${q(p)}`).join("&")}`,

  downloadUrl: (path: string) => `/api/download?path=${q(path)}`,
  rawUrl: (path: string) => `/api/raw?path=${q(path)}`,
};
