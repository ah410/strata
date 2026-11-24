import { bytes } from "../lib/format";
import { timeLeft } from "../lib/rate";
import { useTransfer } from "../lib/useTransfer";

/** A running server job, such as extracting or compressing. */
export interface RunningJob {
  id: string;
  title: string;
  done: number;
  total: number;
}

/** Live progress for a background job: bytes, percent, speed and time left. */
export function JobPanel({ job, onCancel }: { job: RunningJob; onCancel: () => void }) {
  const { shown, speed } = useTransfer(job.done);
  const known = job.total > 0;
  const pct = known ? Math.min(100, (shown / job.total) * 100) : 0;
  return (
    <div className="upload-panel" role="status" aria-live="polite">
      <div className="upload-top">
        <strong title={job.title}>{job.title}</strong>
        <button className="btn" onClick={onCancel}>Cancel</button>
      </div>
      <div className="upload-bar"><span style={{ width: `${pct}%` }} /></div>
      <div className="upload-nums">
        <span>{known ? `${bytes(shown)} of ${bytes(job.total)}` : "Preparing…"}</span>
        {known && <span>{Math.floor(pct)}%</span>}
      </div>
      <div className="upload-nums dim">
        <span>{speed > 0 ? `${bytes(speed)}/s` : known ? "Measuring speed…" : ""}</span>
        <span>{timeLeft(job.total - job.done, speed)}</span>
      </div>
    </div>
  );
}
