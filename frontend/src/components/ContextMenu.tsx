import { useEffect, useLayoutEffect, useRef, useState } from "react";

export interface MenuItem {
  id: string;
  label: string;
  group: number;
  danger?: boolean;
  hint?: string;
}

/** Top-left for a w×h menu opened at (x, y): flipped or clamped to stay on screen. */
export function place(x: number, y: number, w: number, h: number, vw: number, vh: number, pad = 6) {
  const left = x + w + pad > vw ? Math.max(pad, x - w) : x;
  const top = y + h + pad > vh ? Math.max(pad, vh - h - pad) : y;
  return { left, top };
}

/** Strata's right-click menu. Arrow keys move, Enter picks, Escape closes. */
export function ContextMenu({
  x, y, items, onPick, onClose,
}: {
  x: number;
  y: number;
  items: MenuItem[];
  onPick: (id: string) => void;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ left: x, top: y });
  const [active, setActive] = useState(0);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    setPos(place(x, y, el.offsetWidth, el.offsetHeight, window.innerWidth, window.innerHeight));
    el.focus();
  }, [x, y]);

  useEffect(() => {
    const away = (e: Event) => {
      if (!ref.current?.contains(e.target as Node)) onClose();
    };
    const close = () => onClose();
    window.addEventListener("mousedown", away, true);
    window.addEventListener("contextmenu", away, true);
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    window.addEventListener("blur", close);
    return () => {
      window.removeEventListener("mousedown", away, true);
      window.removeEventListener("contextmenu", away, true);
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
      window.removeEventListener("blur", close);
    };
  }, [onClose]);

  const pick = (id: string) => {
    onClose();
    onPick(id);
  };

  const onKey = (e: React.KeyboardEvent) => {
    const n = items.length;
    const step: Record<string, number> = { ArrowDown: 1, ArrowUp: -1 };
    if (e.key in step) setActive((i) => (i + step[e.key] + n) % n);
    else if (e.key === "Home") setActive(0);
    else if (e.key === "End") setActive(n - 1);
    else if (e.key === "Enter" || e.key === " ") items[active] && pick(items[active].id);
    else if (e.key === "Escape" || e.key === "Tab") onClose();
    else return;
    e.preventDefault();
    e.stopPropagation();
  };

  return (
    <div
      ref={ref}
      className="menu"
      role="menu"
      tabIndex={-1}
      style={{ left: pos.left, top: pos.top }}
      onKeyDown={onKey}
      onContextMenu={(e) => e.preventDefault()}
    >
      {items.map((it, i) => (
        <div key={it.id} style={{ display: "contents" }}>
          {i > 0 && items[i - 1].group !== it.group && <div className="menu-sep" role="separator" />}
          <button
            role="menuitem"
            className={`menu-item${i === active ? " active" : ""}${it.danger ? " danger" : ""}`}
            tabIndex={-1}
            onMouseEnter={() => setActive(i)}
            onClick={() => pick(it.id)}
          >
            <span>{it.label}</span>
            {it.hint && <kbd>{it.hint}</kbd>}
          </button>
        </div>
      ))}
    </div>
  );
}
