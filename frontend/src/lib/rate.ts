/**
 * Transfer speed over the last few seconds. A long upload's speed drifts
 * (Wi-Fi, other traffic), so a recent window beats the overall average.
 */
export class Rate {
  private samples: { t: number; bytes: number }[] = [];
  constructor(private windowMs = 8000) {}

  add(bytes: number, t = Date.now()) {
    this.samples.push({ t, bytes });
    while (this.samples.length > 2 && t - this.samples[1].t > this.windowMs) this.samples.shift();
  }

  /** Bytes per second, or 0 until there are two seconds of samples. */
  perSecond(): number {
    const a = this.samples[0];
    const b = this.samples[this.samples.length - 1];
    if (!a || b.t - a.t < 2000) return 0;
    return Math.max(0, ((b.bytes - a.bytes) * 1000) / (b.t - a.t));
  }
}

/** "about 34 min left" style estimate, or "" while unknown. */
export function timeLeft(remaining: number, perSecond: number): string {
  if (perSecond <= 0) return "";
  const s = remaining / perSecond;
  if (s < 5) return "a few seconds left";
  if (s < 60) return `${Math.round(s / 5) * 5} s left`;
  const m = Math.round(s / 60);
  if (m < 60) return `about ${m} min left`;
  const h = Math.floor(m / 60);
  return `about ${h} h ${m % 60 ? `${m % 60} min ` : ""}left`;
}
