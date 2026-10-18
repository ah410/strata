import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, type Entry, type Listing } from "./lib/api";
import { bytes, joinPath, parentOf, when } from "./lib/format";
import { Capacity } from "./components/Capacity";
import { Glyph } from "./components/Glyph";
import { Preview, canPreview } from "./components/Preview";
import { SheetEditor } from "./components/SheetEditor";
import { AppearanceDialog } from "./components/Appearance";

type SortKey = "name" | "size" | "modified" | "kind";
type Note = { text: string; error?: boolean } | null;

export default function App({ onSignOut }: { onSignOut?: () => void }) {
  const [path, setPath] = useState(() => decodeURIComponent(location.hash.slice(1)));
  const [listing, setListing] = useState<Listing | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: "name", desc: false });

  const [open, setOpen] = useState<Entry | null>(null);
  const [prompt, setPrompt] = useState<null | { title: string; label: string; value: string; onOk: (v: string) => void }>(null);
  const [note, setNote] = useState<Note>(null);
  const [uploadPct, setUploadPct] = useState<number | null>(null);
  const [dragging, setDragging] = useState(false);
  const [railOpen, setRailOpen] = useState(false);
  const [appearanceOpen, setAppearanceOpen] = useState(false);

  const fileInput = useRef<HTMLInputElement>(null);

  const refresh = useCallback(() => {
    api.list(path).then(
      (l) => {
        setListing(l);
        setError(null);
        setPicked(new Set());
      },
      (e: Error) => {
        setError(e.message);
        setListing(null);
      }
    );
  }, [path]);

  useEffect(() => {
    refresh();
    history.replaceState(null, "", `#${encodeURIComponent(path)}`);
  }, [refresh, path]);

  // Browser back/forward should walk the directory history.
  useEffect(() => {
    const onHash = () => setPath(decodeURIComponent(location.hash.slice(1)));
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  const toast = (text: string, error = false) => {
    setNote({ text, error });
    setTimeout(() => setNote(null), error ? 5200 : 2600);
  };

  const run = async (fn: () => Promise<unknown>, ok: string) => {
    try {
      await fn();
      toast(ok);
      refresh();
    } catch (e) {
      toast((e as Error).message, true);
    }
  };

  const navigate = (to: string) => {
    setPath(to);
    setRailOpen(false);
    history.pushState(null, "", `#${encodeURIComponent(to)}`);
  };

  const activate = (e: Entry) => {
    if (e.isDir) return navigate(e.path);
    if (e.kind === "sheet") return setOpen(e);
    if (canPreview(e.kind)) return setOpen(e);
    window.location.href = api.downloadUrl(e.path);
  };

  const rows = useMemo(() => {
    const list = [...(listing?.entries ?? [])];
    const dir = sort.desc ? -1 : 1;
    list.sort((a, b) => {
      // Directories stay grouped above files regardless of sort column.
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      switch (sort.key) {
        case "size":     return (a.size - b.size) * dir;
        case "modified": return (Date.parse(a.modified) - Date.parse(b.modified)) * dir;
        case "kind":     return a.kind.localeCompare(b.kind) * dir;
        default:         return a.name.localeCompare(b.name, undefined, { numeric: true }) * dir;
      }
    });
    return list;
  }, [listing, sort]);

  const crumbs = useMemo(() => {
    const parts = path ? path.split("/") : [];
    return parts.map((name, i) => ({ name, path: parts.slice(0, i + 1).join("/") }));
  }, [path]);

  const upload = async (files: FileList | File[]) => {
    // Copy now: the file input is cleared right after this call, which
    // empties a live FileList before the upload finishes.
    const list = Array.from(files);
    setUploadPct(0);
    try {
      await api.upload(path, list, setUploadPct);
      toast(`Uploaded ${list.length} file${list.length === 1 ? "" : "s"}`);
      refresh();
    } catch (e) {
      toast((e as Error).message, true);
    } finally {
      setUploadPct(null);
    }
  };

  const removePicked = () => {
    const targets = [...picked];
    if (!targets.length) return;
    setPrompt({
      title: `Delete ${targets.length} item${targets.length === 1 ? "" : "s"}?`,
      label: "",
      value: "",
      onOk: () =>
        run(async () => {
          for (const p of targets) await api.remove(p);
        }, `Deleted ${targets.length} item${targets.length === 1 ? "" : "s"}`),
    });
  };

  const renamePicked = () => {
    const target = [...picked][0];
    if (!target) return;
    const current = target.split("/").pop() ?? "";
    setPrompt({
      title: "Rename",
      label: "New name",
      value: current,
      onOk: (v) => {
        const next = joinPath(parentOf(target), v.trim());
        if (!v.trim() || next === target) return;
        run(() => api.move(target, next), `Renamed to ${v.trim()}`);
      },
    });
  };

  const newFolder = () =>
    setPrompt({
      title: "New folder",
      label: "Folder name",
      value: "",
      onOk: (v) => {
        if (!v.trim()) return;
        run(() => api.mkdir(joinPath(path, v.trim())), `Created ${v.trim()}`);
      },
    });

  return (
    <div className="shell">
      <aside className={`rail${railOpen ? " open" : ""}`}>
        <div className="rail-head">
          <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
            <rect x="1.5" y="2.5" width="13" height="3.4" rx="1" fill="var(--k-folder)" opacity=".85" />
            <rect x="1.5" y="6.3" width="13" height="3.4" rx="1" fill="var(--k-sheet)" opacity=".7" />
            <rect x="1.5" y="10.1" width="13" height="3.4" rx="1" fill="var(--k-video)" opacity=".55" />
          </svg>
          <span className="wordmark">Strata</span>
          <button
            className="btn btn-icon"
            onClick={() => setAppearanceOpen(true)}
            aria-label="Appearance"
            title="Appearance"
          >
            <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true">
              <circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" strokeWidth="1.4" />
              <path d="M8 2a6 6 0 0 1 0 12z" fill="currentColor" />
            </svg>
          </button>
          {onSignOut && (
            <button className="btn btn-icon" onClick={onSignOut} aria-label="Sign out" title="Sign out">
              <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.4">
                <path d="M6 2.5H3.5v11H6M10.5 5l3 3-3 3M13.5 8H6.5" />
              </svg>
            </button>
          )}
        </div>

        <nav className="rail-nav">
          <div className="rail-group">
            <div className="rail-label">Library</div>
            <button
              className="rail-item"
              aria-current={path === ""}
              onClick={() => navigate("")}
            >
              <span className="dot" style={{ background: "var(--k-folder)" }} />
              All files
            </button>
          </div>

          {listing && listing.entries.some((e) => e.isDir) && (
            <div className="rail-group">
              <div className="rail-label">In this folder</div>
              {listing.entries
                .filter((e) => e.isDir)
                .slice(0, 12)
                .map((e) => (
                  <button key={e.path} className="rail-item" onClick={() => navigate(e.path)}>
                    <span className="dot" style={{ background: "var(--k-folder)", opacity: 0.5 }} />
                    {e.name}
                  </button>
                ))}
            </div>
          )}
        </nav>

        <Capacity />
      </aside>

      <main className="main">
        <div className="topbar">
          <button
            className="btn btn-icon rail-toggle"
            onClick={() => setRailOpen((v) => !v)}
            aria-label="Toggle sidebar"
          >
            ☰
          </button>

          <div className="crumbs">
            <button className="crumb" onClick={() => navigate("")}>
              /
            </button>
            {crumbs.map((c, i) => (
              <span key={c.path} style={{ display: "contents" }}>
                {i > 0 && <span className="crumb-sep">/</span>}
                <button className="crumb" onClick={() => navigate(c.path)}>
                  {c.name}
                </button>
              </span>
            ))}
          </div>

          <div className="actions">
            {picked.size === 1 && (
              <button className="btn" onClick={renamePicked}>
                Rename
              </button>
            )}
            {picked.size > 0 && (
              <button className="btn danger" onClick={removePicked}>
                Delete
              </button>
            )}
            <button className="btn" onClick={newFolder}>
              New folder
            </button>
            <button className="btn primary" onClick={() => fileInput.current?.click()}>
              Upload
            </button>
          </div>
        </div>

        {uploadPct !== null && <div className="progress" style={{ width: `${uploadPct}%` }} />}

        <div
          className={`listing${dragging ? " dropping" : ""}`}
          onDragOver={(e) => {
            e.preventDefault();
            setDragging(true);
          }}
          onDragLeave={(e) => {
            if (e.currentTarget === e.target) setDragging(false);
          }}
          onDrop={(e) => {
            e.preventDefault();
            setDragging(false);
            if (e.dataTransfer.files.length) upload(e.dataTransfer.files);
          }}
        >
          {error && (
            <div className="state error">
              <h2>Could not read this folder</h2>
              <p>{error}</p>
            </div>
          )}

          {!error && listing && rows.length === 0 && (
            <div className="state">
              <h2>Nothing here yet</h2>
              <p>Drop files anywhere in this pane, or use Upload.</p>
            </div>
          )}

          {!error && rows.length > 0 && (
            <table className="table">
              <thead>
                <tr>
                  <th className="col-pick" />
                  <SortHead label="Name" k="name" sort={sort} setSort={setSort} />
                  <SortHead label="Size" k="size" sort={sort} setSort={setSort} num />
                  <SortHead label="Modified" k="modified" sort={sort} setSort={setSort} />
                  <SortHead label="Kind" k="kind" sort={sort} setSort={setSort} />
                </tr>
              </thead>
              <tbody>
                {rows.map((e) => (
                  <tr key={e.path} className={`row${picked.has(e.path) ? " selected" : ""}`}>
                    <td className="col-pick">
                      <input
                        type="checkbox"
                        className="pick"
                        checked={picked.has(e.path)}
                        aria-label={`Select ${e.name}`}
                        onChange={(ev) =>
                          setPicked((s) => {
                            const next = new Set(s);
                            ev.target.checked ? next.add(e.path) : next.delete(e.path);
                            return next;
                          })
                        }
                      />
                    </td>
                    <td>
                      <div className="name">
                        <span className="glyph">
                          <Glyph kind={e.kind} />
                        </span>
                        <button onClick={() => activate(e)}>{e.name}</button>
                      </div>
                    </td>
                    <td className="col-size">{e.isDir ? "—" : bytes(e.size)}</td>
                    <td className="col-when">{when(e.modified)}</td>
                    <td className="col-kind">{e.isDir ? "folder" : e.kind}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </main>

      <input
        ref={fileInput}
        type="file"
        multiple
        hidden
        onChange={(e) => {
          if (e.target.files?.length) upload(e.target.files);
          e.target.value = "";
        }}
      />

      {open && open.kind === "sheet" && (
        <SheetEditor entry={open} onClose={() => { setOpen(null); refresh(); }} />
      )}
      {open && open.kind !== "sheet" && <Preview entry={open} onClose={() => setOpen(null)} />}

      {appearanceOpen && <AppearanceDialog onClose={() => setAppearanceOpen(false)} />}

      {prompt && (
        <Dialog
          {...prompt}
          onClose={() => setPrompt(null)}
        />
      )}

      {note && (
        <div className={`toast${note.error ? " err" : ""}`} role="status">
          <span className="toast-dot" />
          {note.text}
        </div>
      )}
    </div>
  );
}

function SortHead({
  label, k, sort, setSort, num,
}: {
  label: string;
  k: SortKey;
  sort: { key: SortKey; desc: boolean };
  setSort: (s: { key: SortKey; desc: boolean }) => void;
  num?: boolean;
}) {
  const active = sort.key === k;
  return (
    <th
      className={[num ? "num" : "", `col-${k === "modified" ? "when" : k}`].filter(Boolean).join(" ")}
      onClick={() => setSort({ key: k, desc: active ? !sort.desc : false })}
      aria-sort={active ? (sort.desc ? "descending" : "ascending") : "none"}
    >
      {label}
      {active && <span className="sort-caret">{sort.desc ? "↓" : "↑"}</span>}
    </th>
  );
}

function Dialog({
  title, label, value, onOk, onClose,
}: {
  title: string;
  label: string;
  value: string;
  onOk: (v: string) => void;
  onClose: () => void;
}) {
  const [v, setV] = useState(value);
  const ref = useRef<HTMLInputElement>(null);

  useEffect(() => {
    ref.current?.focus();
    ref.current?.select();
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const confirm = () => {
    onOk(v);
    onClose();
  };

  return (
    <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog" role="dialog" aria-modal="true" aria-label={title}>
        <h3>{title}</h3>
        {label ? (
          <>
            <p>{label}</p>
            <input
              ref={ref}
              value={v}
              onChange={(e) => setV(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && confirm()}
            />
          </>
        ) : (
          <p>This cannot be undone.</p>
        )}
        <div className="dialog-actions">
          <button className="btn" onClick={onClose}>
            Cancel
          </button>
          <button className="btn primary" onClick={confirm}>
            {label ? "Save" : "Delete"}
          </button>
        </div>
      </div>
    </div>
  );
}
