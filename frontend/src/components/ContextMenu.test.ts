import { describe, expect, it } from "vitest";
import { place } from "./ContextMenu";

describe("place", () => {
  it("opens at the pointer when it fits", () => {
    expect(place(100, 100, 200, 300, 1280, 800)).toEqual({ left: 100, top: 100 });
  });

  it("flips left near the right edge", () => {
    expect(place(1200, 100, 200, 300, 1280, 800).left).toBe(1000);
  });

  it("lifts up near the bottom edge", () => {
    expect(place(100, 700, 200, 300, 1280, 800).top).toBe(494);
  });

  it("never goes off the top-left on a tiny screen", () => {
    const p = place(50, 50, 400, 900, 320, 600);
    expect(p.left).toBeGreaterThanOrEqual(6);
    expect(p.top).toBeGreaterThanOrEqual(6);
  });
});
