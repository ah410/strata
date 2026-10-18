import { useEffect, useState } from "react";
import { api, type Version } from "../lib/api";
import { bytes } from "../lib/format";

/**
 * Earlier copies Strata kept when it replaced this file. Restoring is itself
 * kept as a version, so nothing here can lose work.
 */
export function History({ path, onClose, onRestored }: { path: string; onClose: () => void; onRestored: () => void }) {
  const [versions, setVersions] = useState<Version[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  useEffect(() => {
    api.versions(path).then(setVersions, (e: Error) => setError(e.message));
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [path, onClose]);

  const restore = async (v: Version) => {
    setBusy(v.id);
    try {
      await api.restoreVersion(path, v.id);
      onRestored();
      onClose();
    } catch (e) {
      setError((e as Error).message);
      setBusy(null);
    }
  };

  return (
    <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog wide" role="dialog" aria-modal="true" aria-label="Version history">
        <h3>Version history</h3>
        <p>
          {path.split("/").pop()} · earlier copies are kept each time Strata replaces this file (the last 20, up to 90
          days).
        </p>
        {error && <p className="login-error">{error}</p>}
        {versions && versions.length === 0 && <p>No earlier versions yet.</p>}
        {versions && versions.length > 0 && (
          <div className="history">
            {versions.map((v) => (
              <div className="field-row" key={v.id}>
                <span>
                  {new Date(v.time).toLocaleString()} <span className="muted">· {bytes(v.size)}</span>
                </span>
                <span style={{ display: "flex", gap: 6 }}>
                  <a className="btn" href={api.versionUrl(path, v.id)} download>
                    Download
                  </a>
                  <button className="btn" disabled={busy !== null} onClick={() => restore(v)}>
                    {busy === v.id ? "Restoring…" : "Restore"}
                  </button>
                </span>
              </div>
            ))}
          </div>
        )}
        <div className="dialog-actions" style={{ marginTop: 14 }}>
          <button className="btn" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
