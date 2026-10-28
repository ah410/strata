import { describe, expect, it } from "vitest";
import { Rate, timeLeft } from "./rate";

describe("Rate", () => {
  it("waits for two seconds of samples, then measures the window", () => {
    const r = new Rate(8000);
    r.add(0, 0);
    r.add(1_000_000, 1000);
    expect(r.perSecond()).toBe(0);
    r.add(4_000_000, 4000);
    expect(r.perSecond()).toBe(1_000_000);
  });

  it("forgets old samples so the speed follows changes", () => {
    const r = new Rate(4000);
    r.add(0, 0);
    r.add(10_000_000, 1000); // a fast burst
    for (let t = 2000; t <= 20000; t += 1000) r.add(10_000_000 + (t - 1000) * 100, t); // then 100 KB/s
    expect(Math.round(r.perSecond())).toBe(100_000);
  });
});

describe("timeLeft", () => {
  it("rounds to what a person wants to know", () => {
    expect(timeLeft(100, 0)).toBe("");
    expect(timeLeft(3, 1)).toBe("a few seconds left");
    expect(timeLeft(42, 1)).toBe("40 s left");
    expect(timeLeft(34 * 60, 1)).toBe("about 34 min left");
    expect(timeLeft(125 * 60, 1)).toBe("about 2 h 5 min left");
    expect(timeLeft(120 * 60, 1)).toBe("about 2 h left");
  });
});
