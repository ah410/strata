import { useEffect, useRef, useState } from "react";
import { api, type Entry } from "../lib/api";
import { Glyph } from "./Glyph";
import { DocPreview, PEEKABLE } from "./DocPreview";
import { CARD_SIZES, setPref, usePrefs } from "../lib/prefs";

const THUMBED = /\.(jpe?g|png|gif|webp|bmp)$/i;

/** Clamps and stores a card size step. */
function setCardSize(n: number) {
  setPref("cardSize", Math.max(0, Math.min(CARD_SIZES.length - 1, n)));
}

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
  const { cardSize } = usePrefs();
  const size = CARD_SIZES[cardSize] ?? CARD_SIZES[1];
  const ref = useRef<HTMLDivElement>(null);

  // Ctrl/Cmd + scroll resizes cards, like an OS file manager. React's wheel
  // listener is passive, so this one is attached by hand to stop page zoom.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    let acc = 0;
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) return;
      e.preventDefault();
      acc += e.deltaY;
      if (Math.abs(acc) < 60) return;
      setCardSize(cardSize + (acc < 0 ? 1 : -1));
      acc = 0;
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [cardSize]);

  return (
    <div ref={ref} className="grid-view" style={{ "--card": `${size.px}px`, "--k": size.scale } as React.CSSProperties}>
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
              <Art entry={e} px={size.px} />
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

/** The toolbar slider for grid card size. */
export function CardSizeSlider() {
  const { cardSize } = usePrefs();
  const label = `Card size: ${CARD_SIZES[cardSize]?.label ?? ""}`;
  return (
    <input
      type="range"
      className="card-size"
      min={0}
      max={CARD_SIZES.length - 1}
      step={1}
      value={cardSize}
      onChange={(e) => setCardSize(Number(e.target.value))}
      aria-label={label}
      title={`${label} (Ctrl+scroll over the grid also works)`}
    />
  );
}

function Art({ entry, px }: { entry: Entry; px: number }) {
  const [failed, setFailed] = useState(false);
  const { previews } = usePrefs();
  // Cards stretch up to about 1.5x their minimum; ask for a sharper image when that outgrows 256px.
  const shown = px * 1.5;
  const large = shown * (window.devicePixelRatio || 1) > 256;
  if (!entry.isDir && !failed) {
    if (previews && PEEKABLE.test(entry.name)) return <DocPreview entry={entry} width={Math.round(shown)} onFail={() => setFailed(true)} />;
    // SVG renders directly in <img>, which never runs its scripts.
    if (/\.svg$/i.test(entry.name)) return <img src={api.rawUrl(entry.path)} alt="" loading="lazy" onError={() => setFailed(true)} />;
    if (THUMBED.test(entry.name)) return <img src={api.thumbUrl(entry, large)} alt="" loading="lazy" onError={() => setFailed(true)} />;
  }
  return <Glyph kind={entry.kind} />;
}
