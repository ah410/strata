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

export interface TextFile {
  content: string;
  truncated: boolean;
  version: string;
}

export interface AppState {
  /** False without a config volume: nothing is remembered. */
  enabled: boolean;
  favorites: Entry[];
  recent: (Entry & { opened: string })[];
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

export interface Job<R = unknown> {
  id: string;
  kind: string;
  state: "queued" | "running" | "done" | "failed" | "canceled";
  done: number;
  total: number;
  result?: R;
  error?: string;
}

export interface SizeEntry {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  files?: number;
  kind: Kind;
}

export interface SizeReport {
  ready: boolean;
  scanning: boolean;
  seen: number;
  scanned: string;
  path: string;
  size: number;
  files: number;
  hidden: number;
  children: SizeEntry[];
  largest: SizeEntry[];
  kinds: Partial<Record<Kind, number>>;
  trash: number;
  versions: number;
}

export interface DocPeek {
  type: "text" | "table" | "doc" | "slides";
  lines?: string[];
  rows?: string[][];
  blocks?: { text: string; heading?: boolean }[];
  slides?: number;
}

export interface FileInfo extends Entry {
  type?: string;
  items?: number;
  width?: number;
  height?: number;
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

/**
 * The message to show for a failed response. Proxies answer with HTML pages,
 * which make no sense in a toast, so those become a plain sentence.
 */
export function errorText(status: number, body: string, fallback = `${status} error`): string {
  const text = body.trim();
  if (/^<|<html|<body/i.test(text)) {
    if (status === 413) return "The reverse proxy in front of Strata rejected the request as too large";
    if (status === 502 || status === 503 || status === 504) return "Strata didn't answer in time (the proxy returned " + status + ")";
    return `The server returned an error page (${status})`;
  }
  return text || fallback;
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
    throw new ApiError(errorText(res.status, await res.text(), `${res.status} ${res.statusText}`), res.status);
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
  sizes: (path: string, rescan = false) =>
    req<SizeReport>(`/api/usage/tree?path=${q(path)}${rescan ? "&rescan=1" : ""}`),
  text: (path: string) => req<TextFile>(`/api/text?path=${q(path)}`),

  /** Saves text; an empty version creates a new file. Resolves to the new version. */
  saveText: (path: string, version: string, content: string) =>
    req<{ version: string }>("/api/text", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path, version, content }),
    }).then((r) => r.version),

  /** The mtime in the URL means a changed image gets a fresh thumbnail. */
  thumbUrl: (e: Entry) => `/api/thumb?path=${q(e.path)}&m=${q(e.modified)}`,

  /** A document's text peek for its grid card; the mtime keys the cache. */
  peek: (e: Entry) => req<DocPeek>(`/api/preview?path=${q(e.path)}&m=${q(e.modified)}`),

  state: () => req<AppState>("/api/state"),

  favorite: (path: string, on: boolean) =>
    req<{ ok: true }>("/api/favorite", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path, on }),
    }),

  /** Records an opened file; failures don't matter to the user. */
  opened: (path: string) =>
    req<{ ok: true }>("/api/recent", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path }),
    }).catch(() => undefined),

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

  /** Starts unpacking an archive; resolves to the job id. */
  extract: (path: string) =>
    req<{ job: string }>("/api/extract", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path }),
    }).then((r) => r.job),

  /** Starts zipping paths into dir on the share; resolves to the job id. */
  compress: (paths: string[], dir: string) =>
    req<{ job: string }>("/api/compress", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ paths, dir }),
    }).then((r) => r.job),

  info: (path: string) => req<FileInfo>(`/api/info?path=${q(path)}`),

  /** Starts hashing a file; resolves to the job id. */
  checksum: (path: string) =>
    req<{ job: string }>("/api/checksum", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path }),
    }).then((r) => r.job),

  job: <R>(id: string) => req<Job<R>>(`/api/jobs/${q(id)}`),
  cancelJob: (id: string) => fetch(`/api/jobs/${q(id)}`, { method: "DELETE", headers: GUARD }),

  downloadUrl: (path: string) => `/api/download?path=${q(path)}`,
  rawUrl: (path: string) => `/api/raw?path=${q(path)}`,
};
