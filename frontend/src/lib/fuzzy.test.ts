import { describe, expect, it } from "vitest";
import { rank, score } from "./fuzzy";

describe("score", () => {
  it("needs every character in order", () => {
    expect(score("nf", "New folder")).toBeGreaterThan(0);
    expect(score("fn", "New folder")).toBe(-1);
    expect(score("", "anything")).toBe(0);
  });

  it("prefers word starts and runs", () => {
    expect(score("nf", "New folder")).toBeGreaterThan(score("nf", "Info"));
    expect(score("rep", "report.pdf")).toBeGreaterThan(score("rep", "grape_pie.txt"));
  });
});

describe("rank", () => {
  it("orders matches and drops misses", () => {
    const items = ["Info", "New folder", "Rename", "Upload"];
    expect(rank(items, "nf", (s) => s)).toEqual(["New folder", "Info"]);
    expect(rank(items, "", (s) => s)).toEqual(items);
  });
});
