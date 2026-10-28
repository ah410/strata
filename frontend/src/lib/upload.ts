import { ApiError, errorText, signedOut } from "./api";

/** A file plus where it goes, relative to the upload's target folder. */
export interface UploadItem {
  file: File;
  rel: string;
}

// Big files go in resumable chunks; small ones share multipart requests.
// No request carries much more than BATCH_BYTES, so a reverse proxy's body
// limit (often a few MB) doesn't reject a folder of thousands of photos.
const CHUNKED_FROM = 8 << 20;
const BATCH_BYTES = 8 << 20;
const BATCH_FILES = 200;
const MIN_CHUNK = 256 << 10;
const RETRIES = 6;

// Shrinks for the rest of the session once a proxy rejects a chunk as too big.
let chunkCap = 8 << 20;

type Progress = (pct: number) => void;

/** Uploads every item into dir, reporting overall progress. */
export async function uploadAll(dir: string, items: UploadItem[], onProgress?: Progress) {
  const total = items.reduce((n, i) => n + i.file.size, 0) || 1;
  let done = 0;
  const report = (inFlight: number) => onProgress?.(((done + inFlight) / total) * 100);

  const small = items.filter((i) => i.file.size < CHUNKED_FROM);
  const big = items.filter((i) => i.file.size >= CHUNKED_FROM);

  for (const batch of batches(small, BATCH_BYTES, BATCH_FILES)) {
    await multipartOrSplit(dir, batch, (n) => report(n));
    done += batch.reduce((n, i) => n + i.file.size, 0);
  }
  for (const item of big) {
    await chunked(dir, item, report);
    done += item.file.size;
  }
  report(0);
}

/** Groups items into runs of at most maxBytes (a lone bigger file gets its own) and maxFiles. */
export function batches(items: UploadItem[], maxBytes: number, maxFiles: number): UploadItem[][] {
  const out: UploadItem[][] = [];
  let cur: UploadItem[] = [];
  let bytes = 0;
  for (const it of items) {
    if (cur.length && (bytes + it.file.size > maxBytes || cur.length >= maxFiles)) {
      out.push(cur);
      cur = [];
      bytes = 0;
    }
    cur.push(it);
    bytes += it.file.size;
  }
  if (cur.length) out.push(cur);
  return out;
}

// A 413 from the proxy halves the batch; a single file then goes in chunks,
// which shrink until they fit.
async function multipartOrSplit(dir: string, items: UploadItem[], report: (n: number) => void): Promise<void> {
  try {
    await multipart(dir, items, report);
  } catch (e) {
    if (!(e instanceof ApiError) || e.status !== 413) throw e;
    if (items.length === 1) return chunked(dir, items[0], report);
    const mid = Math.ceil(items.length / 2);
    const first = items.slice(0, mid);
    const sent = first.reduce((n, i) => n + i.file.size, 0);
    await multipartOrSplit(dir, first, report);
    await multipartOrSplit(dir, items.slice(mid), (n) => report(sent + n));
  }
}

function multipart(dir: string, items: UploadItem[], report: (n: number) => void) {
  const form = new FormData();
  for (const { file, rel } of items) {
    if (rel !== file.name) form.append("relpath", rel);
    form.append("file", file, file.name);
  }
  // XHR rather than fetch: fetch still can't report upload progress.
  return send("POST", `/api/upload?path=${encodeURIComponent(dir)}`, form, {}, (n) => report(n)).then((x) => {
    if (x.status < 200 || x.status >= 300) throw new ApiError(errorText(x.status, x.text, "Upload failed"), x.status);
  });
}

async function chunked(dir: string, { file, rel }: UploadItem, report: (n: number) => void) {
  // Remembered per file, so a reload or dropped connection resumes.
  const key = `strata:upload:${dir}/${rel}:${file.size}:${file.lastModified}`;
  let id = store.get(key);
  let offset = id ? await head(id) : -1;
  let chunk = chunkCap;
  if (offset < 0) {
    const res = await fetch("/api/uploads", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Strata": "1" },
      body: JSON.stringify({ dir, name: rel, size: file.size }),
    });
    if (res.status === 401) signedOut();
    if (!res.ok) throw new ApiError(errorText(res.status, await res.text(), "Upload failed"), res.status);
    const r = (await res.json()) as { id: string; chunk: number };
    id = r.id;
    chunk = r.chunk || chunk;
    offset = 0;
    store.set(key, id);
  }

  let failures = 0;
  while (offset < file.size) {
    chunk = Math.min(chunk, chunkCap);
    const end = Math.min(offset + chunk, file.size);
    const base = offset;
    try {
      const x = await send("PUT", `/api/uploads/${id}`, file.slice(offset, end), {
        "Upload-Offset": String(offset),
        "Content-Type": "application/offset+octet-stream",
      }, (n) => report(base + n));
      const next = Number(x.offset);
      if (x.status === 204 || x.status === 409) {
        // 409: the server has a different offset (a retried chunk); follow it.
        offset = Number.isFinite(next) && x.offset !== null ? next : end;
        failures = 0;
        continue;
      }
      if (x.status === 413 && chunk > MIN_CHUNK) {
        chunkCap = Math.max(MIN_CHUNK, Math.floor(chunk / 2));
        continue;
      }
      if (x.status === 404) {
        store.del(key);
        throw new ApiError("Upload expired; please try again", 404);
      }
      throw new ApiError(errorText(x.status, x.text, "Upload failed"), x.status);
    } catch (e) {
      if (e instanceof ApiError || ++failures > RETRIES) throw e;
      await sleep(1000 * 2 ** (failures - 1));
      const at = await head(id!);
      if (at < 0) throw new ApiError("Upload expired; please try again", 404);
      offset = at;
    }
  }

  const res = await fetch(`/api/uploads/${id}/finish`, { method: "POST", headers: { "X-Strata": "1" } });
  if (!res.ok) throw new ApiError(errorText(res.status, await res.text(), "Upload failed"), res.status);
  store.del(key);
}

/** The server's offset for an upload, or -1 if it no longer exists. */
async function head(id: string): Promise<number> {
  try {
    const res = await fetch(`/api/uploads/${id}`, { method: "HEAD" });
    const n = Number(res.headers.get("Upload-Offset"));
    return res.ok && Number.isFinite(n) ? n : -1;
  } catch {
    return -1;
  }
}

/** Resolves with the response for any HTTP status; rejects on network failure. */
function send(method: string, url: string, body: XMLHttpRequestBodyInit, headers: Record<string, string>, onSent: (n: number) => void) {
  return new Promise<{ status: number; text: string; offset: string | null }>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(method, url);
    xhr.setRequestHeader("X-Strata", "1");
    for (const [k, v] of Object.entries(headers)) xhr.setRequestHeader(k, v);
    xhr.upload.onprogress = (e) => onSent(e.loaded);
    xhr.onload = () => {
      if (xhr.status === 401) signedOut();
      resolve({ status: xhr.status, text: xhr.responseText.trim(), offset: xhr.getResponseHeader("Upload-Offset") });
    };
    xhr.onerror = () => reject(new Error("network"));
    xhr.send(body);
  });
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// Storage can be missing or throw (private windows); resuming is a bonus.
const store = {
  get: (k: string) => { try { return localStorage.getItem(k); } catch { return null; } },
  set: (k: string, v: string) => { try { localStorage.setItem(k, v); } catch { /* ignore */ } },
  del: (k: string) => { try { localStorage.removeItem(k); } catch { /* ignore */ } },
};

/** Files from a drop, walking any dropped folders. Call synchronously in the handler. */
export function fromDrop(dt: DataTransfer): Promise<UploadItem[]> {
  const entries = Array.from(dt.items)
    .map((i) => i.webkitGetAsEntry?.())
    .filter((e): e is FileSystemEntry => !!e);
  if (!entries.some((e) => e.isDirectory)) {
    return Promise.resolve(Array.from(dt.files).map((file) => ({ file, rel: file.name })));
  }
  return Promise.all(entries.map((e) => walk(e, ""))).then((l) => l.flat());
}

async function walk(entry: FileSystemEntry, prefix: string): Promise<UploadItem[]> {
  const rel = prefix + entry.name;
  if (entry.isFile) {
    const file = await new Promise<File>((res, rej) => (entry as FileSystemFileEntry).file(res, rej));
    return [{ file, rel }];
  }
  const reader = (entry as FileSystemDirectoryEntry).createReader();
  const out: UploadItem[] = [];
  // readEntries returns batches; an empty batch means done.
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((res, rej) => reader.readEntries(res, rej));
    if (!batch.length) break;
    for (const e of batch) out.push(...(await walk(e, rel + "/")));
  }
  return out;
}

/** Files from an <input>, keeping folder structure when one was picked. */
export function fromInput(files: FileList): UploadItem[] {
  return Array.from(files).map((file) => ({ file, rel: file.webkitRelativePath || file.name }));
}
