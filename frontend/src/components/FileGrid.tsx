import { useState } from "react";
import { api, type Entry } from "../lib/api";
import { Glyph } from "./Glyph";

const THUMBED = /\.(jpe?g|png|gif|webp|bmp)$/i;

/** Card view of a folder; images show thumbnails, everything else its glyph. */
export function FileGrid({
  rows, picked, togglePick, activate, favorites, rowProps,
}: {
  rows: Entry[];
  picked: Set<string>;
  togglePick: (path: string, on: boolean) => void;
  activate: (e: Entry) => void;
  favorites: Set<string>;
  /** Drag and drop wiring shared with the list view. */
  rowProps: (e: Entry) => React.HTMLAttributes<HTMLElement> & { draggable?: boolean; className?: string };
}) {
  return (
    <div className="grid-view">
      {rows.map((e) => {
        const p = rowProps(e);
        return (
          <div key={e.path} {...p} className={`card${picked.has(e.path) ? " selected" : ""}${p.className ?? ""}`}>
            <input
              type="checkbox"
              className="pick"
              checked={picked.has(e.path)}
              aria-label={`Select ${e.name}`}
              onChange={(ev) => togglePick(e.path, ev.target.checked)}
            />
            <button className="card-art" onClick={() => activate(e)} aria-label={`Open ${e.name}`} tabIndex={-1}>
              <Art entry={e} />
            </button>
            <button className="card-name link-plain" onClick={() => activate(e)} title={e.name}>
              {e.name}
              {favorites.has(e.path) && <span className="star" aria-label="favorite">★</span>}
            </button>
          </div>
        );
      })}
    </div>
  );
}

function Art({ entry }: { entry: Entry }) {
  const [failed, setFailed] = useState(false);
  if (!entry.isDir && !failed) {
    // SVG renders directly in <img>, which never runs its scripts.
    if (/\.svg$/i.test(entry.name)) return <img src={api.rawUrl(entry.path)} alt="" loading="lazy" onError={() => setFailed(true)} />;
    if (THUMBED.test(entry.name)) return <img src={api.thumbUrl(entry)} alt="" loading="lazy" onError={() => setFailed(true)} />;
  }
  return <Glyph kind={entry.kind} />;
}
