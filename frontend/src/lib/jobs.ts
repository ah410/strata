import { api, type Job } from "./api";

/** Percent done, or null while the total isn't known yet. */
export function percent(j: Pick<Job, "done" | "total">): number | null {
  if (j.total <= 0) return null;
  return Math.min(100, Math.round((j.done / j.total) * 100));
}

/** Polls a job until it ends; resolves with its result or throws its error. */
export async function follow<R>(id: string, onProgress?: (pct: number | null) => void, every = 600): Promise<R> {
  for (;;) {
    const j = await api.job<R>(id);
    if (j.state === "done") return j.result as R;
    if (j.state === "failed") throw new Error(j.error || "The operation failed");
    if (j.state === "canceled") throw new Error("Canceled");
    onProgress?.(percent(j));
    await new Promise((r) => setTimeout(r, every));
  }
}
