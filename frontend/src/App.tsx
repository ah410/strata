import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, ApiError, type AppState, type Entry, type Listing, type SearchResult } from "./lib/api";
import { fromDrop, fromInput, uploadAll, type UploadItem } from "./lib/upload";
import { bytes, freeCopyName, joinPath, parentOf, when } from "./lib/format";
import { Capacity } from "./components/Capacity";
import { Glyph } from "./components/Glyph";
import { Preview, canPreview } from "./components/Preview";
import { SheetEditor } from "./components/SheetEditor";
import { AppearanceDialog } from "./components/Appearance";
import { History } from "./components/History";
import { TrashView } from "./components/TrashView";
import { FolderPicker } from "./components/FolderPicker";
import { FileGrid } from "./components/FileGrid";
import { TextEditor } from "./components/TextEditor";
import { actionsFor, type ActionId, type Ctx } from "./lib/actions";
import { ContextMenu } from "./components/ContextMenu";
import { follow } from "./lib/jobs";
import { box, overlaps, range, type Box } from "./lib/selection";

// Rows dragged inside Strata carry their paths under this type.
const DRAG_TYPE = "application/x-strata-paths";

type SortKey = "name" | "size" | "modified" | "kind";
type Layout = "list" | "grid";

const LAYOUT_KEY = "strata:layout";
const savedLayout = (): Layout => {
  try { return localStorage.getItem(LAYOUT_KEY) === "grid" ? "grid" : "list"; } catch { return "list"; }
};
type Note = { text: string; error?: boolean; action?: { label: string; run: () => void } } | null;

export default function App({ onSignOut }: { onSignOut?: () => void }) {
  const [path, setPath] = useState(() => decodeURIComponent(location.hash.slice(1)));
  const [view, setView] = useState<"files" | "trash" | "recent">("files");
  const [layout, setLayoutState] = useState<Layout>(savedLayout);
  const [saved, setSaved] = useState<AppState | null>(null);
  const [listing, setListing] = useState<Listing | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: "name", desc: false });

  const [open, setOpen] = useState<Entry | null>(null);
  const [prompt, setPrompt] = useState<null | { title: string; label: string; value: string; onOk: (v: string) => void }>(null);
  const [note, setNote] = useState<Note>(null);
  const [progress, setProgress] = useState<number | null>(null);
  const [dragging, setDragging] = useState(false);
  const [railOpen, setRailOpen] = useState(false);
  const [appearanceOpen, setAppearanceOpen] = useState(false);
  const [historyPath, setHistoryPath] = useState<string | null>(null);
  const closeHistory = useCallback(() => setHistoryPath(null), []);
  const noteTimer = useRef<number>();
  const [query, setQuery] = useState("");
  const [found, setFound] = useState<SearchResult | null>(null);
  const [searching, setSearching] = useState(false);
  const [picker, setPicker] = useState<null | { mode: "move" | "copy"; items: Entry[] }>(null);
  const [dropTarget, setDropTarget] = useState<string | null>(null);
  const [menu, setMenu] = useState<null | { x: number; y: number; sel: Entry[] }>(null);
  const closeMenu = useCallback(() => setMenu(null), []);
  const anchor = useRef<string | null>(null);
  const [clip, setClip] = useState<null | { cut: boolean; items: Entry[] }>(null);
  const [marquee, setMarquee] = useState<Box | null>(null);

  const loadSaved = useCallback(() => { api.state().then(setSaved, () => undefined); }, []);
  useEffect(loadSaved, [loadSaved]);
  const favorites = useMemo(() => new Set(saved?.favorites.map((e) => e.path)), [saved]);

  const setLayout = (l: Layout) => {
    setLayoutState(l);
    try { localStorage.setItem(LAYOUT_KEY, l); } catch { /* per-device nicety only */ }
  };

  const fileInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);

  const [searchTick, setSearchTick] = useState(0);
  const refresh = useCallback(() => {
    setSearchTick((n) => n + 1);
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

  // Also on returning from the Trash view, which may have restored files.
  useEffect(() => {
    if (view !== "files") return;
    refresh();
    history.replaceState(null, "", `#${encodeURIComponent(path)}`);
  }, [refresh, path, view]);

  // Search the current folder as the user types; a newer query cancels the last.
  useEffect(() => {
    const q = query.trim();
    if (q.length < 2) {
      setFound(null);
      setSearching(false);
      return;
    }
    const ctl = new AbortController();
    setSearching(true);
    const t = window.setTimeout(() => {
      api.search(path, q, ctl.signal).then(
        (r) => { setFound(r); setPicked(new Set()); setSearching(false); },
        (e: Error) => { if (e.name !== "AbortError") { setError(e.message); setSearching(false); } }
      );
    }, 250);
    return () => { window.clearTimeout(t); ctl.abort(); };
  }, [query, path, searchTick]);

  // Browser back/forward should walk the directory history.
  useEffect(() => {
    const onHash = () => setPath(decodeURIComponent(location.hash.slice(1)));
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  // Stable, so views can depend on it; each toast resets the dismiss timer.
  const toast = useCallback((text: string, error = false, action?: { label: string; run: () => void }) => {
    setNote({ text, error, action });
    window.clearTimeout(noteTimer.current);
    noteTimer.current = window.setTimeout(() => setNote(null), action ? 7000 : error ? 5200 : 2600);
  }, []);

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
    setQuery("");
    setView("files");
    setPath(to);
    setRailOpen(false);
    history.pushState(null, "", `#${encodeURIComponent(to)}`);
  };

  const activate = (e: Entry) => {
    if (e.isDir) return navigate(e.path);
    api.opened(e.path).then(loadSaved);
    if (e.kind === "sheet" || canPreview(e.kind)) return setOpen(e);
    window.location.href = api.downloadUrl(e.path);
  };

  const togglePick = (p: string, on: boolean) => {
    anchor.current = p;
    setPicked((s) => {
      const next = new Set(s);
      on ? next.add(p) : next.delete(p);
      return next;
    });
  };

  type Mods = { shiftKey: boolean; ctrlKey: boolean; metaKey: boolean };
  const hasMods = (ev: Mods) => ev.shiftKey || ev.ctrlKey || ev.metaKey;
  // Click selects one, Ctrl/Cmd toggles, Shift extends from the last click.
  const pick = (p: string, ev: Mods, add = ev.ctrlKey || ev.metaKey) => {
    if (ev.shiftKey) {
      const span = range(rows.map((e) => e.path), anchor.current, p);
      setPicked((s) => new Set([...(add ? s : []), ...span]));
      return;
    }
    if (add) return togglePick(p, !picked.has(p));
    anchor.current = p;
    setPicked(new Set([p]));
  };
  // Names and thumbnails open on a plain click and select with a modifier.
  const openOrPick = (e: Entry, ev: React.MouseEvent) => {
    ev.stopPropagation();
    if (hasMods(ev)) pick(e.path, ev);
    else activate(e);
  };
  // A Shift-click on a checkbox adds the whole range.
  const checkClick = (e: Entry, ev: React.MouseEvent) => {
    if (!ev.shiftKey) return;
    ev.preventDefault();
    pick(e.path, ev, true);
  };

  // Dragging on empty space draws a box that selects what it touches.
  const startMarquee = (ev: React.MouseEvent) => {
    if (ev.button !== 0 || (ev.target as Element).closest("[data-path], button, input, a, thead, .search-note")) return;
    const x0 = ev.clientX, y0 = ev.clientY;
    const base = hasMods(ev) ? new Set(picked) : new Set<string>();
    const items = [...document.querySelectorAll<HTMLElement>(".listing [data-path]")].map((el) => ({
      path: el.dataset.path!,
      rect: el.getBoundingClientRect(),
    }));
    let moved = false;
    const move = (m: MouseEvent) => {
      if (!moved && Math.abs(m.clientX - x0) + Math.abs(m.clientY - y0) < 5) return;
      moved = true;
      const b = box(x0, y0, m.clientX, m.clientY);
      setMarquee(b);
      setPicked(new Set([...base, ...items.filter((i) => overlaps(b, i.rect)).map((i) => i.path)]));
    };
    const up = () => {
      window.removeEventListener("mousemove", move);
      window.removeEventListener("mouseup", up);
      setMarquee(null);
      if (!moved && !hasMods(ev)) setPicked(new Set());
    };
    window.addEventListener("mousemove", move);
    window.addEventListener("mouseup", up);
    ev.preventDefault();
  };

  const star = async (targets: string[]) => {
    const on = !targets.every((p) => favorites.has(p));
    try {
      for (const p of targets) await api.favorite(p, on);
      toast(on ? "Added to favorites" : "Removed from favorites");
    } catch (e) {
      toast((e as Error).message, true);
    }
    loadSaved();
  };

  const newTextFile = () =>
    setPrompt({
      title: "New text file",
      label: "File name",
      value: "Untitled.md",
      onOk: async (v) => {
        const name = v.trim();
        if (!name) return;
        const p = joinPath(path, name);
        try {
          await api.saveText(p, "", "");
          refresh();
          activate({ name, path: p, isDir: false, size: 0, modified: new Date().toISOString(), kind: "text" });
        } catch (e) {
          toast((e as Error).message, true);
        }
      },
    });

  const searchMode = found !== null;
  const shown = searchMode ? found.entries : listing?.entries ?? [];
  const pickedEntries = shown.filter((e) => picked.has(e.path));

  const rows = useMemo(() => {
    const list = [...shown];
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
  }, [listing, found, sort]);

  const crumbs = useMemo(() => {
    const parts = path ? path.split("/") : [];
    return parts.map((name, i) => ({ name, path: parts.slice(0, i + 1).join("/") }));
  }, [path]);

  const upload = async (list: UploadItem[]) => {
    if (!list.length) return;
    setProgress(0);
    try {
      await uploadAll(path, list, setProgress);
      toast(`Uploaded ${list.length} file${list.length === 1 ? "" : "s"}`);
      refresh();
    } catch (e) {
      toast((e as Error).message, true);
    } finally {
      setProgress(null);
    }
  };

  // Deleting moves to the trash at once; the toast offers Undo. Only a
  // location without a trash asks before deleting permanently.
  const remove = async (targets: string[]) => {
    if (!targets.length) return;
    const ids: string[] = [];
    const noTrash: string[] = [];
    for (const p of targets) {
      try {
        const r = await api.remove(p);
        if (r.trashId) ids.push(r.trashId);
      } catch (e) {
        if (e instanceof ApiError && e.status === 409) noTrash.push(p);
        else toast((e as Error).message, true);
      }
    }
    refresh();
    if (ids.length) {
      const n = ids.length;
      toast(`Moved ${n} item${n === 1 ? "" : "s"} to trash`, false, {
        label: "Undo",
        run: async () => {
          for (const id of ids) await api.restore(id).catch(() => undefined);
          toast("Restored");
          refresh();
        },
      });
    }
    if (noTrash.length) {
      setPrompt({
        title: `Delete ${noTrash.length} item${noTrash.length === 1 ? "" : "s"} permanently?`,
        label: "",
        value: "",
        onOk: () =>
          run(async () => {
            for (const p of noTrash) await api.remove(p, true);
          }, "Deleted permanently"),
      });
    }
  };

  const plural = (n: number, w: string) => `${n} ${w}${n === 1 ? "" : "s"}`;

  // Moves or copies each path into dest, keeping its name.
  const transfer = async (paths: string[], dest: string, copy: boolean) => {
    const todo = paths.filter((p) => copy || parentOf(p) !== dest);
    if (!todo.length) return;
    let ok = 0;
    for (const p of todo) {
      if (dest === p || dest.startsWith(p + "/")) {
        toast(`Can't ${copy ? "copy" : "move"} a folder into itself`, true);
        continue;
      }
      try {
        const to = joinPath(dest, p.split("/").pop()!);
        await (copy ? api.copy(p, to) : api.move(p, to));
        ok++;
      } catch (e) {
        toast(`${p.split("/").pop()}: ${(e as Error).message}`, true);
      }
    }
    if (ok) toast(`${copy ? "Copied" : "Moved"} ${plural(ok, "item")} to /${dest}`);
    refresh();
    if (!copy) loadSaved();
  };

  // Runs a server job with the top progress bar, then refreshes.
  const runJob = async <R,>(start: Promise<string>, done: (r: R) => void) => {
    setProgress(0);
    try {
      done(await follow<R>(await start, (p) => setProgress(p ?? 0)));
      refresh();
    } catch (e) {
      toast((e as Error).message, true);
    } finally {
      setProgress(null);
    }
  };

  const extract = (e: Entry) => {
    toast(`Extracting ${e.name}…`);
    runJob<string>(api.extract(e.path), (dest) =>
      toast(`Extracted to ${dest.split("/").pop()}`, false, { label: "Open", run: () => navigate(dest) }));
  };

  // Pastes the clipboard into dest. Names that are taken get " (copy)",
  // so pasting into the same folder duplicates instead of failing.
  const paste = async (dest: string) => {
    if (!clip) return;
    const taken = new Set((dest === path && listing ? listing.entries : (await api.list(dest).catch(() => null))?.entries ?? []).map((e) => e.name));
    let ok = 0;
    for (const { path: p, isDir } of clip.items) {
      if (dest === p || dest.startsWith(p + "/")) {
        toast(`Can't paste a folder into itself`, true);
        continue;
      }
      if (clip.cut && parentOf(p) === dest) continue;
      const name = freeCopyName(p.split("/").pop()!, taken, isDir);
      try {
        await (clip.cut ? api.move(p, joinPath(dest, name)) : api.copy(p, joinPath(dest, name)));
        taken.add(name);
        ok++;
      } catch (e) {
        toast(`${p.split("/").pop()}: ${(e as Error).message}`, true);
      }
    }
    if (ok) toast(`${clip.cut ? "Moved" : "Copied"} ${plural(ok, "item")} to /${dest}`);
    if (clip.cut) setClip(null);
    refresh();
    if (clip.cut) loadSaved();
  };

  const download = (sel: Entry[]) => {
    if (sel.length === 1 && !sel[0].isDir) window.location.href = api.downloadUrl(sel[0].path);
    else if (sel.length) window.location.href = api.zipUrl(sel.map((e) => e.path));
  };

  const dragPaths = (e: Entry) => (picked.has(e.path) ? [...picked] : [e.path]);
  // Drag source on every item, drop target on folders; shared by both layouts.
  const rowProps = (e: Entry) => ({
    draggable: true,
    "data-path": e.path,
    "data-cut": clip?.cut && clip.items.some((c) => c.path === e.path) ? "" : undefined,
    onClick: (ev: React.MouseEvent) => {
      if (!(ev.target as Element).closest("button, input, a")) pick(e.path, ev);
    },
    onContextMenu: (ev: React.MouseEvent) => {
      ev.stopPropagation();
      // Right-clicking outside the selection acts on that item alone.
      const sel = picked.has(e.path) ? pickedEntries : [e];
      if (!picked.has(e.path)) setPicked(new Set([e.path]));
      openMenu(ev, sel);
    },
    className: dropTarget === e.path ? " drop-on" : "",
    onDragStart: (ev: React.DragEvent) => {
      ev.dataTransfer.setData(DRAG_TYPE, JSON.stringify(dragPaths(e)));
      ev.dataTransfer.effectAllowed = "copyMove";
    },
    onDragEnd: () => setDropTarget(null),
    ...(e.isDir ? dropProps(e.path) : {}),
  });
  // Shift+right-click keeps the browser's own menu as an escape hatch.
  const openMenu = (ev: React.MouseEvent, sel: Entry[]) => {
    if (ev.shiftKey) return;
    ev.preventDefault();
    let { clientX: x, clientY: y } = ev;
    if (!x && !y) {
      // Menu key or Shift+F10: open at the element instead of the corner.
      const r = (ev.currentTarget as Element).getBoundingClientRect();
      x = r.left + 24;
      y = r.top + r.height / 2;
    }
    setMenu({ x, y, sel });
  };

  const dropProps = (dest: string) => ({
    onDragOver: (ev: React.DragEvent) => {
      if (!ev.dataTransfer.types.includes(DRAG_TYPE)) return;
      ev.preventDefault();
      ev.stopPropagation();
      ev.dataTransfer.dropEffect = ev.ctrlKey || ev.altKey ? "copy" : "move";
      setDropTarget(dest);
    },
    onDragLeave: () => setDropTarget((t) => (t === dest ? null : t)),
    onDrop: (ev: React.DragEvent) => {
      if (!ev.dataTransfer.types.includes(DRAG_TYPE)) return;
      ev.preventDefault();
      ev.stopPropagation();
      setDropTarget(null);
      const paths = JSON.parse(ev.dataTransfer.getData(DRAG_TYPE) || "[]") as string[];
      transfer(paths.filter((p) => p !== dest), dest, ev.ctrlKey || ev.altKey);
    },
  });

  const rename = (target: string) => {
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

  const ctx = (selection: Entry[]): Ctx => ({ selection, canStar: !!saved?.enabled, favorites, layout, clipboard: clip?.items.length ?? 0 });

  // What each registry action does, applied to an explicit selection.
  const perform = (id: ActionId, sel: Entry[]) => {
    const paths = sel.map((e) => e.path);
    switch (id) {
      case "open": return sel[0] && activate(sel[0]);
      case "extract": return sel[0] && extract(sel[0]);
      case "rename": return sel[0] && rename(sel[0].path);
      case "history": return sel[0] && setHistoryPath(sel[0].path);
      case "star": return star(paths);
      case "cut": case "clip":
        setClip({ cut: id === "cut", items: sel });
        return toast(`${id === "cut" ? "Cut" : "Copied"} ${plural(paths.length, "item")}. Paste in another folder.`);
      case "paste": return paste(path);
      case "pasteInto": return sel[0] && paste(sel[0].path);
      case "move": case "copy": return setPicker({ mode: id, items: sel });
      case "download": return download(sel);
      case "delete": return remove(paths);
      case "layout": return setLayout(layout === "list" ? "grid" : "list");
      case "newFolder": return newFolder();
      case "newFile": return newTextFile();
      case "uploadFolder": return folderInput.current?.click();
      case "upload": return fileInput.current?.click();
      case "selectAll": return setPicked(new Set(rows.map((e) => e.path)));
      case "refresh": return refresh();
    }
  };

  return (
    <div className="shell">
      <aside className={`rail${railOpen ? " open" : ""}`}>
        <div className="rail-head">
          <button className="home" onClick={() => navigate("")} aria-label="Strata: all files" title="All files">
            <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
              <rect x="1.5" y="2.5" width="13" height="3.4" rx="1" fill="var(--k-folder)" opacity=".85" />
              <rect x="1.5" y="6.3" width="13" height="3.4" rx="1" fill="var(--k-sheet)" opacity=".7" />
              <rect x="1.5" y="10.1" width="13" height="3.4" rx="1" fill="var(--k-video)" opacity=".55" />
            </svg>
            <span className="wordmark">Strata</span>
          </button>
          <button
            className="btn btn-icon"
            onClick={() => setAppearanceOpen(true)}
            aria-label="Settings"
            title="Settings"
          >
            <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.4">
              <circle cx="8" cy="8" r="2.2" />
              <path d="M8 1.5v2M8 12.5v2M1.5 8h2M12.5 8h2M3.4 3.4l1.4 1.4M11.2 11.2l1.4 1.4M3.4 12.6l1.4-1.4M11.2 4.8l1.4-1.4" strokeLinecap="round" />
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
              aria-current={view === "files" && path === ""}
              onClick={() => navigate("")}
            >
              <span className="dot" style={{ background: "var(--k-folder)" }} />
              All files
            </button>
            {saved?.enabled && (
              <button className="rail-item" aria-current={view === "recent"} onClick={() => { setView("recent"); setRailOpen(false); }}>
                <span className="dot" style={{ background: "var(--k-text)" }} />
                Recent
              </button>
            )}
            <button className="rail-item" aria-current={view === "trash"} onClick={() => { setView("trash"); setRailOpen(false); }}>
              <span className="dot" style={{ background: "var(--k-file)" }} />
              Trash
            </button>
          </div>

          {saved && saved.favorites.length > 0 && (
            <div className="rail-group">
              <div className="rail-label">Favorites</div>
              {saved.favorites.map((e) => (
                <button key={e.path} className="rail-item" title={e.path} onClick={() => { setRailOpen(false); activate(e); }}>
                  <span className="star" style={{ margin: 0 }}>★</span>
                  {e.name}
                </button>
              ))}
            </div>
          )}

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

        <Capacity onNavigate={navigate} onOpenTrash={() => { setView("trash"); setRailOpen(false); }} />
      </aside>

      <main className="main">
        {view === "trash" ? (
          <TrashView toast={toast} />
        ) : view === "recent" ? (
          <RecentView recent={saved?.recent ?? []} activate={activate} navigate={navigate} />
        ) : (
        <>
        <div className="topbar">
          <button
            className="btn btn-icon rail-toggle"
            onClick={() => setRailOpen((v) => !v)}
            aria-label="Toggle sidebar"
          >
            ☰
          </button>

          <div className="crumbs">
            <button className={`crumb${dropTarget === "" ? " drop-on" : ""}`} onClick={() => navigate("")} {...dropProps("")}>
              /
            </button>
            {crumbs.map((c, i) => (
              <span key={c.path} style={{ display: "contents" }}>
                {i > 0 && <span className="crumb-sep">/</span>}
                <button
                  className={`crumb${dropTarget === c.path ? " drop-on" : ""}`}
                  onClick={() => navigate(c.path)}
                  {...dropProps(c.path)}
                >
                  {c.name}
                </button>
              </span>
            ))}
          </div>

          <div className="search">
            <input
              type="search"
              placeholder={path ? `Search in ${path.split("/").pop()}` : "Search all files"}
              aria-label="Search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => e.key === "Escape" && setQuery("")}
            />
          </div>

          <div className="actions">
            {actionsFor(ctx(pickedEntries), "toolbar").map((a) => {
              const c = ctx(pickedEntries);
              return a.id === "layout" ? (
                <button key={a.id} className="btn btn-icon" onClick={() => perform(a.id, [])} aria-label={a.label(c)} title={a.label(c)}>
                  {layout === "list" ? "▦" : "☰"}
                </button>
              ) : (
                <button
                  key={a.id}
                  className={`btn${a.primary ? " primary" : ""}${a.danger ? " danger" : ""}`}
                  onClick={() => perform(a.id, pickedEntries)}
                >
                  {a.label(c)}
                </button>
              );
            })}
          </div>
        </div>

        {progress !== null && <div className="progress" style={{ width: `${progress}%` }} />}

        <div
          className={`listing${dragging ? " dropping" : ""}`}
          onMouseDown={startMarquee}
          onContextMenu={(e) => {
            setPicked(new Set());
            openMenu(e, []);
          }}
          onDragOver={(e) => {
            if (!e.dataTransfer.types.includes("Files")) return;
            e.preventDefault();
            setDragging(true);
          }}
          onDragLeave={(e) => {
            if (e.currentTarget === e.target) setDragging(false);
          }}
          onDrop={(e) => {
            if (!e.dataTransfer.types.includes("Files")) return;
            e.preventDefault();
            setDragging(false);
            fromDrop(e.dataTransfer).then(upload, (err: Error) => toast(err.message, true));
          }}
        >
          {error && (
            <div className="state error">
              <h2>Could not read this folder</h2>
              <p>{error}</p>
            </div>
          )}

          {searchMode && (
            <div className="search-note">
              {searching ? "Searching…" : `${found.entries.length} match${found.entries.length === 1 ? "" : "es"}${found.truncated ? " (showing the first; narrow the search)" : ""}`}
              <button className="btn" style={{ height: 24 }} onClick={() => setQuery("")}>Clear</button>
            </div>
          )}

          {!error && searchMode && rows.length === 0 && !searching && (
            <div className="state">
              <h2>No matches</h2>
              <p>Search looks at file and folder names below this folder.</p>
            </div>
          )}

          {!error && !searchMode && listing && rows.length === 0 && (
            <div className="state">
              <h2>Nothing here yet</h2>
              <p>Drop files anywhere in this pane, or use Upload.</p>
            </div>
          )}

          {!error && rows.length > 0 && layout === "grid" && (
            <FileGrid rows={rows} picked={picked} togglePick={togglePick} openOrPick={openOrPick} checkClick={checkClick} favorites={favorites} rowProps={rowProps} />
          )}

          {!error && rows.length > 0 && layout === "list" && (
            <table className="table">
              <thead>
                <tr>
                  <th className="col-pick">
                    <input
                      type="checkbox"
                      className="pick"
                      aria-label="Select all"
                      checked={rows.length > 0 && rows.every((e) => picked.has(e.path))}
                      onChange={(ev) => setPicked(ev.target.checked ? new Set(rows.map((e) => e.path)) : new Set())}
                    />
                  </th>
                  <SortHead label="Name" k="name" sort={sort} setSort={setSort} />
                  <SortHead label="Size" k="size" sort={sort} setSort={setSort} num />
                  <SortHead label="Modified" k="modified" sort={sort} setSort={setSort} />
                  {searchMode ? <th className="col-where">Folder</th> : <SortHead label="Kind" k="kind" sort={sort} setSort={setSort} />}
                </tr>
              </thead>
              <tbody>
                {rows.map((e) => (
                  <tr key={e.path} {...rowProps(e)} className={`row${picked.has(e.path) ? " selected" : ""}${rowProps(e).className}`}>
                    <td className="col-pick">
                      <input
                        type="checkbox"
                        className="pick"
                        checked={picked.has(e.path)}
                        aria-label={`Select ${e.name}`}
                        onClick={(ev) => checkClick(e, ev)}
                        onChange={(ev) => togglePick(e.path, ev.target.checked)}
                      />
                    </td>
                    <td>
                      <div className="name">
                        <span className="glyph">
                          <Glyph kind={e.kind} />
                        </span>
                        <button onClick={(ev) => openOrPick(e, ev)}>{e.name}</button>
                        {favorites.has(e.path) && <span className="star" aria-label="favorite">★</span>}
                      </div>
                    </td>
                    <td className="col-size">{e.isDir ? "—" : bytes(e.size)}</td>
                    <td className="col-when">{when(e.modified)}</td>
                    {searchMode ? (
                      <td className="col-where">
                        <button className="link" onClick={() => navigate(parentOf(e.path))}>/{parentOf(e.path)}</button>
                      </td>
                    ) : (
                      <td className="col-kind">{e.isDir ? "folder" : e.kind}</td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
        </>
        )}
      </main>

      <input
        ref={fileInput}
        type="file"
        multiple
        hidden
        onChange={(e) => {
          // Copy before clearing: clearing empties the live FileList.
          if (e.target.files?.length) upload(fromInput(e.target.files));
          e.target.value = "";
        }}
      />
      <input
        ref={folderInput}
        type="file"
        hidden
        {...{ webkitdirectory: "" }}
        onChange={(e) => {
          if (e.target.files?.length) upload(fromInput(e.target.files));
          e.target.value = "";
        }}
      />

      {picker && (
        <FolderPicker
          title={`${picker.mode === "move" ? "Move" : "Copy"} ${plural(picker.items.length, "item")} to…`}
          action={picker.mode === "move" ? "Move" : "Copy"}
          start={path}
          blocked={picker.items.filter((e) => e.isDir).map((e) => e.path)}
          onPick={(dest) => transfer(picker.items.map((e) => e.path), dest, picker.mode === "copy")}
          onClose={() => setPicker(null)}
        />
      )}

      {open && open.kind === "sheet" && (
        <SheetEditor entry={open} onClose={() => { setOpen(null); refresh(); }} />
      )}
      {open && (open.kind === "text" || open.kind === "csv") && (
        <TextEditor entry={open} onClose={() => { setOpen(null); refresh(); }} />
      )}
      {open && open.kind !== "sheet" && open.kind !== "text" && open.kind !== "csv" && (
        <Preview entry={open} onClose={() => setOpen(null)} />
      )}

      {marquee && (
        <div className="marquee" style={{ left: marquee.left, top: marquee.top, width: marquee.right - marquee.left, height: marquee.bottom - marquee.top }} />
      )}

      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          items={actionsFor(ctx(menu.sel), "menu").map((a) => ({ id: a.id, label: a.label(ctx(menu.sel)), group: a.group, danger: a.danger }))}
          onPick={(id) => perform(id as ActionId, menu.sel)}
          onClose={closeMenu}
        />
      )}

      {appearanceOpen && <AppearanceDialog onClose={() => setAppearanceOpen(false)} />}
      {historyPath && <History path={historyPath} onClose={closeHistory} onRestored={() => { toast("Version restored"); refresh(); }} />}

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
          {note.action && (
            <button
              className="btn"
              style={{ height: 24, marginLeft: 6 }}
              onClick={() => {
                const a = note.action!;
                setNote(null);
                a.run();
              }}
            >
              {note.action.label}
            </button>
          )}
        </div>
      )}
    </div>
  );
}

function RecentView({
  recent, activate, navigate,
}: {
  recent: AppState["recent"];
  activate: (e: Entry) => void;
  navigate: (p: string) => void;
}) {
  return (
    <>
      <div className="topbar">
        <div className="crumbs"><span className="crumb">Recent files</span></div>
      </div>
      <div className="listing">
        {recent.length === 0 ? (
          <div className="state">
            <h2>Nothing opened yet</h2>
            <p>Files you open or edit in Strata show up here.</p>
          </div>
        ) : (
          <table className="table">
            <thead>
              <tr><th>Name</th><th className="col-where">Folder</th><th className="col-when">Opened</th></tr>
            </thead>
            <tbody>
              {recent.map((e) => (
                <tr key={e.path} className="row">
                  <td>
                    <div className="name">
                      <span className="glyph"><Glyph kind={e.kind} /></span>
                      <button onClick={() => activate(e)}>{e.name}</button>
                    </div>
                  </td>
                  <td className="col-where">
                    <button className="link" onClick={() => navigate(parentOf(e.path))}>/{parentOf(e.path)}</button>
                  </td>
                  <td className="col-when">{when(e.opened)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </>
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
