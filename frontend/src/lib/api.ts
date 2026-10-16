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
}

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);
  if (!res.ok) {
    throw new Error((await res.text()).trim() || `${res.status} ${res.statusText}`);
  }
  return res.json() as Promise<T>;
}

const q = (p: string) => encodeURIComponent(p);

export const api = {
  list: (path: string) => req<Listing>(`/api/list?path=${q(path)}`),
  usage: () => req<Usage>("/api/usage"),
  text: (path: string) => req<{ content: string; truncated: boolean }>(`/api/text?path=${q(path)}`),

  sheet: (path: string, sheet?: string) =>
    req<Grid>(`/api/sheet?path=${q(path)}${sheet ? `&sheet=${q(sheet)}` : ""}`),

  saveSheet: (path: string, sheet: string, edits: { ref: string; value: string }[]) =>
    req<Grid>("/api/sheet", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path, sheet, edits }),
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

  remove: (path: string) =>
    req<{ ok: true }>("/api/delete", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path }),
    }),

  upload: (dir: string, files: FileList | File[], onProgress?: (pct: number) => void) =>
    new Promise<void>((resolve, reject) => {
      const form = new FormData();
      for (const f of Array.from(files)) form.append("file", f, f.name);

      // XHR rather than fetch, because fetch still cannot report upload
      // progress and these are NAS-sized files.
      const xhr = new XMLHttpRequest();
      xhr.open("POST", `/api/upload?path=${q(dir)}`);
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable && onProgress) onProgress((e.loaded / e.total) * 100);
      };
      xhr.onload = () =>
        xhr.status >= 200 && xhr.status < 300
          ? resolve()
          : reject(new Error(xhr.responseText.trim() || `Upload failed (${xhr.status})`));
      xhr.onerror = () => reject(new Error("Upload failed"));
      xhr.send(form);
    }),

  downloadUrl: (path: string) => `/api/download?path=${q(path)}`,
  rawUrl: (path: string) => `/api/raw?path=${q(path)}`,
};
