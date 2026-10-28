import { useEffect, useState } from "react";
import { api, type Usage } from "../lib/api";
import { bytes } from "../lib/format";
import { StorageDashboard } from "./Storage";

const SEGMENTS = 24;

/**
 * Pool capacity, drawn as a segmented strip rather than a continuous bar —
 * the readout a drive array gives you on its front panel. For a NAS this is
 * the number you open the thing to check, so it gets the space to be read at
 * a glance from across the room.
 */
export function Capacity({ onNavigate, onOpenTrash }: { onNavigate: (p: string) => void; onOpenTrash: () => void }) {
  const [usage, setUsage] = useState<Usage | null>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    let alive = true;
    const load = () =>
      api.usage().then(
        (u) => alive && setUsage(u),
        () => {} // a failed statfs should not take the sidebar down
      );
    load();
    const t = setInterval(load, 60_000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, []);

  if (!usage || usage.total === 0) {
    return (
      <div className="capacity">
        <div className="capacity-top">
          <span className="capacity-label">Capacity</span>
        </div>
        <div className="segments">
          {Array.from({ length: SEGMENTS }, (_, i) => (
            <span key={i} className="segment" />
          ))}
        </div>
      </div>
    );
  }

  const ratio = usage.used / usage.total;
  const pct = Math.round(ratio * 100);
  const lit = Math.round(ratio * SEGMENTS);
  const state = ratio >= 0.9 ? "crit" : ratio >= 0.75 ? "warn" : "on";

  return (
    <>
    <button className="capacity" onClick={() => setOpen(true)} aria-haspopup="dialog" title="See what's using space">
      <div className="capacity-top">
        <span className="capacity-label">Capacity</span>
        <span className="capacity-pct">{pct}%</span>
      </div>

      <div
        className="segments"
        role="meter"
        aria-valuenow={pct}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label={`Pool ${pct} percent full`}
      >
        {Array.from({ length: SEGMENTS }, (_, i) => (
          <span key={i} className={`segment ${i < lit ? state : ""}`} />
        ))}
      </div>

      <div className="capacity-detail">
        {bytes(usage.free)} free of {bytes(usage.total)}
      </div>
    </button>
    {open && <StorageDashboard usage={usage} onClose={() => setOpen(false)} onNavigate={onNavigate} onOpenTrash={onOpenTrash} />}
    </>
  );
}
