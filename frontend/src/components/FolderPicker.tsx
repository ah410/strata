import { useEffect, useState } from "react";
import { api, type Entry } from "../lib/api";
import { joinPath, parentOf } from "../lib/format";

/** Choose a destination folder for Move or Copy. */
export function FolderPicker({
  title, action, start, blocked, onPick, onClose,
}: {
  title: string;
  action: string;
  start: string;
  /** Folders being moved: they and their insides can't be the destination. */
  blocked: string[];
  onPick: (dest: string) => void;
  onClose: () => void;
}) {
  const [dir, setDir] = useState(start);
  const [folders, setFolders] = useState<Entry[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setFolders(null);
    api.list(dir).then(
      (l) => { setFolders(l.entries.filter((e) => e.isDir)); setError(null); },
      (e: Error) => setError(e.message)
    );
  }, [dir]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const inside = (p: string) => blocked.some((b) => p === b || p.startsWith(b + "/"));
  const parts = dir ? dir.split("/") : [];

  const [naming, setNaming] = useState<string | null>(null);
  const newFolder = async () => {
    const name = naming?.trim();
    setNaming(null);
    if (!name) return;
    try {
      await api.mkdir(joinPath(dir, name));
      setDir(joinPath(dir, name));
    } catch (e) {
      setError((e as Error).message);
    }
  };

  return (
    <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog wide" role="dialog" aria-modal="true" aria-label={title}>
        <h3>{title}</h3>
        <div className="crumbs picker-crumbs">
          <button className="crumb" onClick={() => setDir("")}>/</button>
          {parts.map((name, i) => (
            <span key={i} style={{ display: "contents" }}>
              {i > 0 && <span className="crumb-sep">/</span>}
              <button className="crumb" onClick={() => setDir(parts.slice(0, i + 1).join("/"))}>{name}</button>
            </span>
          ))}
        </div>
        {error && <p className="login-error">{error}</p>}
        <div className="history picker-list">
          {dir && (
            <button className="rail-item" onClick={() => setDir(parentOf(dir))}>
              <span className="dot" style={{ background: "var(--k-folder)", opacity: 0.4 }} />
              ..
            </button>
          )}
          {folders?.map((f) => (
            <button key={f.path} className="rail-item" disabled={inside(f.path)} onClick={() => setDir(f.path)}>
              <span className="dot" style={{ background: "var(--k-folder)" }} />
              {f.name}
            </button>
          ))}
          {folders && folders.length === 0 && <p className="muted" style={{ padding: "6px 8px" }}>No folders here.</p>}
        </div>
        <div className="dialog-actions" style={{ marginTop: 14 }}>
          {naming === null ? (
            <button className="btn" style={{ marginRight: "auto" }} onClick={() => setNaming("")}>New folder</button>
          ) : (
            <input
              autoFocus
              aria-label="New folder name"
              placeholder="Folder name"
              style={{ marginRight: "auto", marginBottom: 0, width: 180 }}
              value={naming}
              onChange={(e) => setNaming(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") newFolder();
                if (e.key === "Escape") { e.stopPropagation(); setNaming(null); }
              }}
              onBlur={() => naming.trim() === "" && setNaming(null)}
            />
          )}
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn primary" disabled={inside(dir)} onClick={() => { onPick(dir); onClose(); }}>
            {action} here
          </button>
        </div>
      </div>
    </div>
  );
}
