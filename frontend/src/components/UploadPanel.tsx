import type { UploadStatus } from "../lib/upload";
import { bytes } from "../lib/format";
import { timeLeft } from "../lib/rate";
import { useTransfer } from "../lib/useTransfer";

/** Live progress for a running upload: bytes, files, speed and time left. */
export function UploadPanel({ status, dir, queued, onCancel }: { status: UploadStatus; dir: string; queued: number; onCancel: () => void }) {
  const { shown, speed } = useTransfer(status.sent);
  const pct = status.total ? (shown / status.total) * 100 : 0;
  return (
    <div className="upload-panel" role="status" aria-live="polite">
      <div className="upload-top">
        <strong>Uploading to /{dir}</strong>
        <button className="btn" onClick={onCancel}>Cancel</button>
      </div>
      <div className="upload-bar"><span style={{ width: `${pct}%` }} /></div>
      <div className="upload-nums">
        <span>{bytes(shown)} of {bytes(status.total)}</span>
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
