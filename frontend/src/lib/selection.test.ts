import { describe, expect, it } from "vitest";
import { box, overlaps, range } from "./selection";

const order = ["a", "b", "c", "d", "e"];

describe("range", () => {
  it("spans either direction inclusively", () => {
    expect(range(order, "b", "d")).toEqual(["b", "c", "d"]);
    expect(range(order, "d", "b")).toEqual(["b", "c", "d"]);
    expect(range(order, "c", "c")).toEqual(["c"]);
  });

  it("falls back to the clicked item without a visible anchor", () => {
    expect(range(order, null, "c")).toEqual(["c"]);
    expect(range(order, "gone", "c")).toEqual(["c"]);
    expect(range(order, "a", "gone")).toEqual([]);
  });
});

describe("box and overlaps", () => {
  it("normalizes a drag in any direction", () => {
    expect(box(10, 50, 0, 20)).toEqual({ left: 0, top: 20, right: 10, bottom: 50 });
  });

  it("hits items the box touches and misses the rest", () => {
    const sel = box(0, 0, 100, 100);
    expect(overlaps(sel, { left: 90, top: 90, right: 200, bottom: 200 })).toBe(true);
    expect(overlaps(sel, { left: 100, top: 0, right: 200, bottom: 50 })).toBe(false);
  });
});
