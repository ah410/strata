import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./api";
import { follow, percent } from "./jobs";

describe("percent", () => {
  it("is unknown without a total and capped at 100", () => {
    expect(percent({ done: 5, total: 0 })).toBeNull();
    expect(percent({ done: 1, total: 4 })).toBe(25);
    expect(percent({ done: 9, total: 4 })).toBe(100);
  });
});

describe("follow", () => {
  afterEach(() => vi.restoreAllMocks());

  it("polls until done and reports progress", async () => {
    const states = [
      { state: "running", done: 1, total: 2 },
      { state: "done", done: 2, total: 2, result: "dl/takeout" },
    ];
    vi.spyOn(api, "job").mockImplementation(async () => ({ id: "j", kind: "extract", ...states.shift()! }) as never);
    const seen: (number | null)[] = [];
    await expect(follow("j", (p) => seen.push(p), 0)).resolves.toBe("dl/takeout");
    expect(seen).toEqual([50]);
  });

  it("throws the job's error", async () => {
    vi.spyOn(api, "job").mockResolvedValue({ id: "j", kind: "x", state: "failed", done: 0, total: 0, error: "the archive is damaged" });
    await expect(follow("j", undefined, 0)).rejects.toThrow("the archive is damaged");
  });
});
