import { useEffect, useRef, useState } from "react";
import type { UploadStatus } from "../lib/upload";
import { bytes } from "../lib/format";
import { Rate, timeLeft } from "../lib/rate";

/** Live progress for a running upload: bytes, files, speed and time left. */
export function UploadPanel({ status, dir, queued, onCancel }: { status: UploadStatus; dir: string; queued: number; onCancel: () => void }) {
  const rate = useRef(new Rate());
  const [speed, setSpeed] = useState(0);

  useEffect(() => {
    rate.current.add(status.sent);
  }, [status.sent]);
  // Speed changes are read once a second so the numbers don't flicker.
  useEffect(() => {
    const t = window.setInterval(() => setSpeed(rate.current.perSecond()), 1000);
    return () => window.clearInterval(t);
  }, []);

  const pct = status.total ? (status.sent / status.total) * 100 : 0;
  return (
    <div className="upload-panel" role="status" aria-live="polite">
      <div className="upload-top">
        <strong>Uploading to /{dir}</strong>
        <button className="btn" onClick={onCancel}>Cancel</button>
      </div>
      <div className="upload-bar"><span style={{ width: `${pct}%` }} /></div>
      <div className="upload-nums">
        <span>{bytes(status.sent)} of {bytes(status.total)}</span>
        <span>{status.done.toLocaleString()} of {status.files.toLocaleString()} files</span>
      </div>
      <div className="upload-nums dim">
        <span>{speed > 0 ? `${bytes(speed)}/s` : "Measuring speed…"}</span>
        <span>{timeLeft(status.total - status.sent, speed)}</span>
      </div>
      {queued > 0 && <div className="upload-nums dim"><span>{queued.toLocaleString()} more file{queued === 1 ? "" : "s"} waiting</span></div>}
    </div>
  );
}
