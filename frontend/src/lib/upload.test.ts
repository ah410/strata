import { afterEach, describe, expect, it, vi } from "vitest";
import { batches, fromPaste, uploadAll, type UploadItem } from "./upload";

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

describe("uploadAll", () => {
  // A fake XHR that answers each request on the next tick with status().
  function fakeServer(status: (n: number) => number) {
    const log = { started: 0, active: 0, peak: 0, afterFailure: 0, failed: false };
    vi.stubGlobal("FormData", class { append() {} });
    vi.stubGlobal(
      "XMLHttpRequest",
      class {
        status = 0;
        responseText = "";
        upload: { onprogress?: (e: { loaded: number }) => void } = {};
        onload?: () => void;
        open() {}
        setRequestHeader() {}
        getResponseHeader() { return null; }
        send() {
          const n = log.started++;
          if (log.failed) log.afterFailure++;
          log.peak = Math.max(log.peak, ++log.active);
          setTimeout(() => {
            log.active--;
            this.status = status(n);
            if (this.status >= 400) log.failed = true;
            this.onload?.();
          }, 1);
        }
      },
    );
    return log;
  }
  const files = (k: number) => Array.from({ length: k }, (_, i) => item(`f${i}`, 5 << 20));
  afterEach(() => vi.unstubAllGlobals());

  it("keeps up to four requests in flight and lands every file", async () => {
    const log = fakeServer(() => 200);
    let landed = 0;
    expect(await uploadAll("/", files(10), { onLanded: () => landed++ })).toBe(10);
    expect(log.peak).toBe(4);
    expect(landed).toBe(10);
  });

  it("starts nothing new once a request fails", async () => {
    const log = fakeServer((n) => (n === 0 ? 500 : 200));
    await expect(uploadAll("/", files(10))).rejects.toThrow();
    expect(log.afterFailure).toBe(0);
    expect(log.started).toBe(4);
  });
});
