import { describe, expect, it } from "vitest";
import { batches, type UploadItem } from "./upload";

const item = (rel: string, size: number): UploadItem => ({ rel, file: { name: rel, size } as File });
const shape = (b: UploadItem[][]) => b.map((g) => g.map((i) => i.rel));

describe("batches", () => {
  it("splits on the byte budget", () => {
    const b = batches([item("a", 4), item("b", 4), item("c", 4)], 8, 100);
    expect(shape(b)).toEqual([["a", "b"], ["c"]]);
  });

  it("splits on the file count", () => {
    const b = batches(Array.from({ length: 5 }, (_, i) => item(`f${i}`, 0)), 8, 2);
    expect(shape(b)).toEqual([["f0", "f1"], ["f2", "f3"], ["f4"]]);
  });

  it("gives an oversized file its own batch instead of dropping it", () => {
    const b = batches([item("a", 2), item("big", 20), item("c", 2)], 8, 100);
    expect(shape(b)).toEqual([["a"], ["big"], ["c"]]);
  });

  it("returns nothing for nothing", () => {
    expect(batches([], 8, 100)).toEqual([]);
  });
});
