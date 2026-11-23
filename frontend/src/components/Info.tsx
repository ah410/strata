import { useEffect, useRef, useState } from "react";
import { api, type FileInfo } from "../lib/api";
import { bytes, parentOf } from "../lib/format";
import { follow } from "../lib/jobs";

/** Details for one item, with an on-demand SHA-256 for files. */
export function InfoDialog({ path, onClose, onNavigate }: { path: string; onClose: () => void; onNavigate: (p: string) => void }) {
  const [info, setInfo] = useState<FileInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sum, setSum] = useState<{ pct: number | null; hex?: string; error?: string } | null>(null);
  const job = useRef<string | null>(null);

  useEffect(() => {
    api.info(path).then(setInfo, (e: Error) => setError(e.message));
  }, [path]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    // Closing mid-hash stops the job instead of leaving it to run.
    return () => {
      window.removeEventListener("keydown", onKey);
      if (job.current) api.cancelJob(job.current);
    };
  }, [onClose]);

  const hash = async () => {
    setSum({ pct: 0 });
    try {
      job.current = await api.checksum(path);
      const hex = await follow<string>(job.current, (pct) => setSum({ pct }), 400);
      setSum({ pct: 100, hex });
    } catch (e) {
      setSum({ pct: null, error: (e as Error).message });
    } finally {
      job.current = null;
    }
  };

  const where = parentOf(path);
  const rows: [string, React.ReactNode][] = info
    ? [
        ["Kind", info.isDir ? "Folder" : info.type || info.kind],
        info.isDir ? ["Contains", `${info.items ?? 0} item${info.items === 1 ? "" : "s"}`] : ["Size", <>{bytes(info.size)} <small>({info.size.toLocaleString()} bytes)</small></>],
        ...(info.width ? [["Dimensions", `${info.width} × ${info.height}`] as [string, React.ReactNode]] : []),
        ["Modified", new Date(info.modified).toLocaleString()],
        ["Location", <button className="link" onClick={() => { onNavigate(where); onClose(); }}>/{where}</button>],
      ]
    : [];

  return (
    <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog info" role="dialog" aria-modal="true" aria-label={`Info for ${path.split("/").pop()}`}>
        <h3 title={path}>{path.split("/").pop()}</h3>
        {error && <p className="dash-error">{error}</p>}
        <dl>
          {rows.map(([k, v]) => (
            <div key={k} className="field-row"><dt>{k}</dt><dd>{v}</dd></div>
          ))}
          {info && !info.isDir && (
            <div className="field-row">
              <dt>SHA-256</dt>
              <dd>
                {!sum && <button className="btn" onClick={hash}>Compute</button>}
                {sum && !sum.hex && !sum.error && <span className="hint">Hashing… {sum.pct ?? 0}%</span>}
                {sum?.error && <span className="dash-error">{sum.error}</span>}
                {sum?.hex && (
                  <span className="sum">
                    <code>{sum.hex}</code>
                    <button className="btn" onClick={() => navigator.clipboard?.writeText(sum.hex!)}>Copy</button>
                  </span>
                )}
              </dd>
            </div>
          )}
        </dl>
        <div className="dialog-actions" style={{ marginTop: 14 }}>
          <button className="btn primary" onClick={onClose}>Done</button>
        </div>
      </div>
    </div>
  );
}
