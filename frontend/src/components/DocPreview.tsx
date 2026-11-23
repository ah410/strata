import { useEffect, useRef, useState } from "react";
import { api, type DocPeek, type Entry } from "../lib/api";
import { pdfThumb } from "../lib/pdfThumb";

/** Which grid cards get a document preview when the setting is on. */
export const PEEKABLE = /\.(pdf|docx|odt|pptx|xlsx|xlsm|csv|tsv|txt|md|log|json|ya?ml|toml|ini|conf|sh|go|js|ts|tsx|jsx|py|java|kt|rs|c|h|cpp|sql|html|css|xml)$/i;

/**
 * A miniature of a document's first page. It loads once the card scrolls
 * into view and calls onFail so the card can fall back to its icon.
 */
export function DocPreview({ entry, onFail }: { entry: Entry; onFail: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(false);
  const [peek, setPeek] = useState<DocPeek | null>(null);
  const [pdf, setPdf] = useState<string | null>(null);
  const isPdf = /\.pdf$/i.test(entry.name);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const io = new IntersectionObserver(([e]) => e.isIntersecting && setVisible(true), { rootMargin: "200px" });
    io.observe(el);
    return () => io.disconnect();
  }, []);

  useEffect(() => {
    if (!visible) return;
    let alive = true;
    const fail = () => alive && onFail();
    if (isPdf) pdfThumb(api.rawUrl(entry.path) + `&m=${encodeURIComponent(entry.modified)}`, 220).then((u) => alive && setPdf(u), fail);
    else api.peek(entry).then((p) => alive && setPeek(p), fail);
    return () => { alive = false; };
  }, [visible, entry.path, entry.modified]);

  return (
    <div ref={ref} className={`peek peek-${isPdf ? "pdf" : peek?.type ?? "loading"}`} aria-hidden="true">
      {pdf && <img src={pdf} alt="" />}
      {peek?.type === "text" && <pre>{peek.lines?.join("\n")}</pre>}
      {peek?.type === "table" && (
        <table>
          <tbody>
            {peek.rows?.map((r, i) => <tr key={i}>{r.map((c, j) => <td key={j}>{c}</td>)}</tr>)}
          </tbody>
        </table>
      )}
      {(peek?.type === "doc" || peek?.type === "slides") && (
        <div className="peek-page">
          {peek.blocks?.map((b, i) => (b.heading ? <strong key={i}>{b.text}</strong> : <p key={i}>{b.text}</p>))}
          {peek.type === "slides" && peek.slides ? <span className="peek-count">{peek.slides} slides</span> : null}
        </div>
      )}
    </div>
  );
}
