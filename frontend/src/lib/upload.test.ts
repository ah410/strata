import { describe, expect, it } from "vitest";
import { batches, fromPaste, type UploadItem } from "./upload";

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

describe("fromPaste", () => {
  const at = new Date(2026, 9, 28, 9, 5, 7);
  const f = (name: string) => ({ name, size: 1 }) as File;

  it("timestamps generic screenshot names", () => {
    expect(fromPaste([f("image.png")], at)[0].rel).toBe("Pasted image 2026-10-28 09.05.07.png");
  });

  it("numbers several pasted images and keeps real names", () => {
    expect(fromPaste([f("image.PNG"), f("image.png"), f("report.pdf")], at).map((i) => i.rel)).toEqual([
      "Pasted image 2026-10-28 09.05.07 (1).png",
      "Pasted image 2026-10-28 09.05.07 (2).png",
      "report.pdf",
    ]);
  });
});
