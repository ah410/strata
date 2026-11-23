import { describe, expect, it } from "vitest";
import { barPct, kindRows } from "./Storage";

describe("kindRows", () => {
  it("sorts by size and drops empty kinds", () => {
    expect(kindRows({ text: 10, video: 500, image: 0, audio: 30 })).toEqual([
      { kind: "video", size: 500 },
      { kind: "audio", size: 30 },
      { kind: "text", size: 10 },
    ]);
  });
});

describe("barPct", () => {
  it("scales to the largest row with a visible minimum", () => {
    expect(barPct(50, 100)).toBe(50);
    expect(barPct(100, 100)).toBe(100);
    expect(barPct(1, 1_000_000)).toBe(1);
    expect(barPct(0, 100)).toBe(0);
    expect(barPct(5, 0)).toBe(0);
  });
});
