import { useCallback, useEffect, useState } from "react";
import { api, type Kind, type SizeEntry, type SizeReport, type Usage } from "../lib/api";
import { bytes, parentOf, when } from "../lib/format";
import { Glyph } from "./Glyph";

const KIND_LABEL: Partial<Record<Kind, string>> = {
  video: "Video", image: "Images", audio: "Audio", doc: "Documents", sheet: "Spreadsheets",
  csv: "CSV", pdf: "PDFs", slides: "Presentations", text: "Text and code", archive: "Archives", file: "Other",
};

/** Kinds by size, largest first, dropping empty ones. */
export function kindRows(kinds: SizeReport["kinds"]): { kind: Kind; size: number }[] {
  return (Object.entries(kinds) as [Kind, number][])
    .filter(([, n]) => n > 0)
    .map(([kind, size]) => ({ kind, size }))
    .sort((a, b) => b.size - a.size);
}

/** Bar width for size against the largest row, never thinner than a sliver. */
export function barPct(size: number, max: number): number {
  if (max <= 0 || size <= 0) return 0;
  return Math.max(1, Math.round((size / max) * 100));
}

/** What fills the share: folders and files by size, drillable, plus trash and history. */
export function StorageDashboard({
  usage, onClose, onNavigate, onOpenTrash,
}: {
  usage: Usage | null;
  onClose: () => void;
  onNavigate: (folder: string) => void;
  onOpenTrash: () => void;
}) {
  const [path, setPath] = useState("");
  const [report, setReport] = useState<SizeReport | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback((rescan = false) => {
    api.sizes(path, rescan).then((r) => { setReport(r); setError(null); }, (e: Error) => setError(e.message));
  }, [path]);

  useEffect(() => load(), [load]);

  // Poll while a scan runs: quickly before the first result, slowly after.
  useEffect(() => {
    if (!report?.scanning) return;
    const t = window.setTimeout(() => load(), report.ready ? 2500 : 800);
    return () => window.clearTimeout(t);
  }, [report, load]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const crumbs = path ? path.split("/") : [];
  const go = (e: SizeEntry) => (e.isDir ? setPath(e.path) : (onNavigate(parentOf(e.path)), onClose()));

  return (
    <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog dash" role="dialog" aria-modal="true" aria-label="Storage">
        <div className="dash-head">
          <h3>Storage</h3>
          <span className="dash-meta">
            {report?.scanning
              ? report.ready ? "Rescanning…" : `Scanning… ${report.seen.toLocaleString()} items`
              : report?.ready && `Scanned ${when(report.scanned)}`}
          </span>
          <button className="btn" disabled={report?.scanning} onClick={() => load(true)}>Rescan</button>
          <button className="btn btn-icon" onClick={onClose} aria-label="Close">✕</button>
        </div>

        {usage && usage.total > 0 && (
          <p className="dash-summary">
            <strong>{bytes(usage.used)}</strong> used of {bytes(usage.total)}, {bytes(usage.free)} free
          </p>
        )}

        <div className="crumbs dash-crumbs">
          <button className="crumb" onClick={() => setPath("")}>All files</button>
          {crumbs.map((name, i) => (
            <span key={i} style={{ display: "contents" }}>
              <span className="crumb-sep">/</span>
              <button className="crumb" onClick={() => setPath(crumbs.slice(0, i + 1).join("/"))}>{name}</button>
            </span>
          ))}
          {path && <button className="btn dash-show" onClick={() => { onNavigate(path); onClose(); }}>Show in files</button>}
        </div>

        {error && <p className="dash-error">{error}</p>}
        {!report?.ready && !error && <div className="dash-wait">Adding up every file. This can take a few minutes on a large share; you can close this and come back.</div>}

        {report?.ready && (
          <div className="dash-cols">
            <section>
              <h4>In this folder <span>{bytes(report.size)} · {report.files.toLocaleString()} files</span></h4>
              <SizeList rows={report.children} onPick={go} empty="Nothing here." />
              {report.hidden > 0 && <p className="hint">Plus {bytes(report.hidden)} of hidden files.</p>}
            </section>
            <section>
              {!path && (
                <>
                  <h4>By type</h4>
                  <ul className="bars">
                    {kindRows(report.kinds).map((k, _, all) => (
                      <li key={k.kind} className="bar-row static" title={`${k.size.toLocaleString()} bytes`}>
                        <span className="bar-name"><span className="glyph"><Glyph kind={k.kind} /></span>{KIND_LABEL[k.kind] ?? k.kind}</span>
                        <span className="bar-track"><span className="bar-fill" style={{ width: `${barPct(k.size, all[0].size)}%` }} /></span>
                        <span className="bar-size">{bytes(k.size)}</span>
                      </li>
                    ))}
                  </ul>
                </>
              )}
              <h4>Largest files</h4>
              <SizeList rows={report.largest} onPick={go} showFolder empty="No files." />
            </section>
          </div>
        )}

        {report?.ready && (
          <div className="dash-foot">
            <span>Trash <strong>{bytes(report.trash)}</strong></span>
            {report.trash > 0 && <button className="btn" onClick={() => { onOpenTrash(); onClose(); }}>Open trash</button>}
            <span>Version history <strong>{bytes(report.versions)}</strong></span>
          </div>
        )}
      </div>
    </div>
  );
}

function SizeList({ rows, onPick, showFolder, empty }: { rows: SizeEntry[]; onPick: (e: SizeEntry) => void; showFolder?: boolean; empty: string }) {
  if (!rows.length) return <p className="hint">{empty}</p>;
  const max = rows[0].size;
  return (
    <ul className="bars">
      {rows.map((e) => (
        <li key={e.path}>
          <button
            className="bar-row"
            onClick={() => onPick(e)}
            title={`${e.path}\n${e.size.toLocaleString()} bytes${e.files ? `, ${e.files.toLocaleString()} files` : ""}`}
          >
            <span className="bar-name">
              <span className="glyph"><Glyph kind={e.kind} /></span>
              <span className="bar-label">{e.name}{showFolder && <small>/{parentOf(e.path)}</small>}</span>
            </span>
            <span className="bar-track"><span className="bar-fill" style={{ width: `${barPct(e.size, max)}%` }} /></span>
            <span className="bar-size">{bytes(e.size)}</span>
          </button>
        </li>
      ))}
    </ul>
  );
}
