import { describe, expect, it } from "vitest";
import { Rate, smoothStep, timeLeft } from "./rate";

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

describe("smoothStep", () => {
  it("moves at the average speed instead of jumping to a burst", () => {
    expect(smoothStep(0, 8e6, 10e6, 100)).toBe(1e6);
  });

  it("never passes the real count and stops when it does", () => {
    expect(smoothStep(9.9e6, 10e6, 10e6, 100)).toBe(10e6);
    expect(smoothStep(10e6, 10e6, 10e6, 100)).toBe(10e6);
  });

  it("catches up faster when far behind", () => {
    expect(smoothStep(0, 100e6, 10e6, 100)).toBe(5e6);
  });

  it("shows the real count until a speed is known or when it went backwards", () => {
    expect(smoothStep(0, 3e6, 0, 100)).toBe(3e6);
    expect(smoothStep(5e6, 0, 10e6, 100)).toBe(0);
  });
});
