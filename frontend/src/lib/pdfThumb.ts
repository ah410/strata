// First-page PDF thumbnails, drawn in the browser with pdf.js. The library
// is a separate chunk, fetched only when previews are on and a PDF is on
// screen, so people who keep previews off never download it.

const cache = new Map<string, Promise<string>>();
let running = 0;
const waiting: (() => void)[] = [];

async function slot<T>(fn: () => Promise<T>): Promise<T> {
  if (running >= 2) await new Promise<void>((r) => waiting.push(r));
  running++;
  try {
    return await fn();
  } finally {
    running--;
    waiting.shift()?.();
  }
}

let lib: Promise<typeof import("pdfjs-dist")> | null = null;
function pdfjs() {
  lib ??= Promise.all([import("pdfjs-dist"), import("pdfjs-dist/build/pdf.worker.min.mjs?url")]).then(([m, w]) => {
    m.GlobalWorkerOptions.workerSrc = w.default;
    return m;
  });
  return lib;
}

/** An object URL for page 1 of the PDF at url, width px wide. Cached per URL. */
export function pdfThumb(url: string, width: number): Promise<string> {
  let p = cache.get(url);
  if (!p) {
    p = slot(() => render(url, width));
    p.catch(() => cache.delete(url));
    cache.set(url, p);
  }
  return p;
}

async function render(url: string, width: number): Promise<string> {
  const { getDocument } = await pdfjs();
  const task = getDocument({ url, useWasm: false, disableAutoFetch: true, disableStream: true });
  try {
    const doc = await task.promise;
    const page = await doc.getPage(1);
    const base = page.getViewport({ scale: 1 });
    const viewport = page.getViewport({ scale: (width * (window.devicePixelRatio || 1)) / base.width });
    const canvas = document.createElement("canvas");
    canvas.width = Math.ceil(viewport.width);
    canvas.height = Math.ceil(viewport.height);
    await page.render({ canvas, viewport }).promise;
    const blob = await new Promise<Blob | null>((r) => canvas.toBlob(r, "image/png"));
    if (!blob) throw new Error("render failed");
    return URL.createObjectURL(blob);
  } finally {
    task.destroy();
  }
}
