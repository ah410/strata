import { useState } from "react";
import { api, type Entry } from "../lib/api";
import { Glyph } from "./Glyph";
import { DocPreview, PEEKABLE } from "./DocPreview";
import { usePrefs } from "../lib/prefs";

const THUMBED = /\.(jpe?g|png|gif|webp|bmp)$/i;

/** Card view of a folder; images show thumbnails, documents a preview when enabled, everything else its glyph. */
export function FileGrid({
  rows, picked, togglePick, openOrPick, checkClick, favorites, rowProps,
}: {
  rows: Entry[];
  picked: Set<string>;
  togglePick: (path: string, on: boolean) => void;
  /** Opens on a plain click, selects with Shift or Ctrl/Cmd. */
  openOrPick: (e: Entry, ev: React.MouseEvent) => void;
  checkClick: (e: Entry, ev: React.MouseEvent) => void;
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
              onClick={(ev) => checkClick(e, ev)}
              onChange={(ev) => togglePick(e.path, ev.target.checked)}
            />
            <button className="card-art" onClick={(ev) => openOrPick(e, ev)} aria-label={`Open ${e.name}`} tabIndex={-1}>
              <Art entry={e} />
            </button>
            <button className="card-name link-plain" onClick={(ev) => openOrPick(e, ev)} title={e.name}>
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
  const { previews } = usePrefs();
  if (!entry.isDir && !failed) {
    if (previews && PEEKABLE.test(entry.name)) return <DocPreview entry={entry} onFail={() => setFailed(true)} />;
    // SVG renders directly in <img>, which never runs its scripts.
    if (/\.svg$/i.test(entry.name)) return <img src={api.rawUrl(entry.path)} alt="" loading="lazy" onError={() => setFailed(true)} />;
    if (THUMBED.test(entry.name)) return <img src={api.thumbUrl(entry)} alt="" loading="lazy" onError={() => setFailed(true)} />;
  }
  return <Glyph kind={entry.kind} />;
}
