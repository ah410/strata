import { describe, expect, it } from "vitest";
import { defaultMarks, freed, keepsOne } from "./Duplicates";

const groups = [
  { size: 100, paths: ["a/x", "b/x", "c/x"], wasted: 200 },
  { size: 10, paths: ["d/y", "e/y"], wasted: 10 },
];

describe("duplicate marks", () => {
  it("marks every copy but the first", () => {
    expect([...defaultMarks(groups)]).toEqual(["b/x", "c/x", "e/y"]);
  });

  it("adds up what trashing frees", () => {
    expect(freed(groups, defaultMarks(groups))).toBe(210);
    expect(freed(groups, new Set(["a/x"]))).toBe(100);
  });

  it("refuses to mark every copy of a file", () => {
    expect(keepsOne(groups, defaultMarks(groups))).toBe(true);
    expect(keepsOne(groups, new Set(["d/y", "e/y"]))).toBe(false);
  });
});
