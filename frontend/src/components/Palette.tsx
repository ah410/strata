import { useEffect, useMemo, useRef, useState } from "react";
import { api, type Entry } from "../lib/api";
import { rank } from "../lib/fuzzy";
import { parentOf } from "../lib/format";
import { Glyph } from "./Glyph";

export interface Command {
  id: string;
  label: string;
  hint?: string;
  section: string;
  run: () => void;
}

/** Ctrl/Cmd+K: run any action or jump to any file or folder by name. */
export function Palette({ commands, onReveal, onClose }: { commands: Command[]; onReveal: (e: Entry) => void; onClose: () => void }) {
  const [q, setQ] = useState("");
  const [found, setFound] = useState<Entry[]>([]);
  const [active, setActive] = useState(0);
  const list = useRef<HTMLUListElement>(null);

  // Names anywhere on the share, from the server's search.
  useEffect(() => {
    if (q.trim().length < 2) return setFound([]);
    const ctl = new AbortController();
    const t = window.setTimeout(() => {
      // Folders first, so each kind sits under one heading.
      api.search("", q.trim(), ctl.signal).then(
        (r) => setFound([...r.entries].sort((a, b) => Number(b.isDir) - Number(a.isDir)).slice(0, 30)),
        () => undefined,
      );
    }, 180);
    return () => { window.clearTimeout(t); ctl.abort(); };
  }, [q]);

  const items = useMemo(() => [
    ...rank(commands, q, (c) => c.label).slice(0, 12),
    ...found.map((e): Command => ({ id: "file:" + e.path, label: e.name, hint: "/" + parentOf(e.path), section: e.isDir ? "Folders" : "Files", run: () => onReveal(e) })),
  ], [commands, found, q]);

  useEffect(() => setActive(0), [q, found]);
  useEffect(() => {
    list.current?.querySelector(`[data-i="${active}"]`)?.scrollIntoView({ block: "nearest" });
  }, [active]);

  const go = (c?: Command) => {
    if (!c) return;
    onClose();
    c.run();
  };

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") setActive((i) => Math.min(i + 1, items.length - 1));
    else if (e.key === "ArrowUp") setActive((i) => Math.max(i - 1, 0));
    else if (e.key === "Enter") go(items[active]);
    else if (e.key === "Escape") onClose();
    else return;
    e.preventDefault();
  };

  const kinds = new Map(found.map((e) => ["file:" + e.path, e]));
  return (
    <div className="scrim palette-scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="palette" role="dialog" aria-modal="true" aria-label="Go to anything">
        <input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={onKey}
          placeholder="Type an action, file or folder…"
          role="combobox"
          aria-expanded="true"
          aria-controls="palette-list"
          aria-activedescendant={items[active] ? `pal-${active}` : undefined}
        />
        <ul ref={list} id="palette-list" role="listbox">
          {items.map((c, i) => {
            const entry = kinds.get(c.id);
            return (
              <li key={c.id} style={{ display: "contents" }}>
                {(i === 0 || items[i - 1].section !== c.section) && <div className="palette-section">{c.section}</div>}
                <div
                  id={`pal-${i}`}
                  data-i={i}
                  role="option"
                  aria-selected={i === active}
                  className={`palette-item${i === active ? " active" : ""}`}
                  onMouseMove={() => setActive(i)}
                  onClick={() => go(c)}
                >
                  {entry && <span className="glyph"><Glyph kind={entry.kind} /></span>}
                  <span className="palette-label">{c.label}</span>
                  {c.hint && (entry ? <small>{c.hint}</small> : <kbd>{c.hint}</kbd>)}
                </div>
              </li>
            );
          })}
          {!items.length && <li className="palette-empty">Nothing matches.</li>}
        </ul>
      </div>
    </div>
  );
}
