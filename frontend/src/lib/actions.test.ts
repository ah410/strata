import { describe, expect, it } from "vitest";
import { actionsFor, type Ctx } from "./actions";
import type { Entry } from "./api";

const file = (path: string): Entry => ({ name: path.split("/").pop()!, path, isDir: false, size: 1, modified: "", kind: "file" });
const dir = (path: string): Entry => ({ ...file(path), isDir: true, kind: "folder" });
const ctx = (selection: Entry[], over: Partial<Ctx> = {}): Ctx => ({
  selection, canStar: true, favorites: new Set(), layout: "list", ...over,
});
const ids = (c: Ctx, where: "toolbar" | "menu" = "menu") => actionsFor(c, where).map((a) => a.id);

describe("actionsFor", () => {
  it("offers folder actions on empty space", () => {
    expect(ids(ctx([]))).toEqual(["layout", "newFolder", "newFile", "uploadFolder", "upload", "selectAll", "refresh"]);
  });

  it("limits single-item actions to one selection", () => {
    expect(ids(ctx([file("a")]))).toContain("rename");
    expect(ids(ctx([file("a"), file("b")]))).not.toContain("rename");
    expect(ids(ctx([file("a"), file("b")]))).not.toContain("open");
  });

  it("shows history for files only", () => {
    expect(ids(ctx([file("a")]))).toContain("history");
    expect(ids(ctx([dir("a")]))).not.toContain("history");
  });

  it("hides star without a config volume and flips its label", () => {
    expect(ids(ctx([file("a")], { canStar: false }))).not.toContain("star");
    const star = (c: Ctx) => actionsFor(c, "menu").find((a) => a.id === "star")!.label(c);
    expect(star(ctx([file("a")]))).toBe("Star");
    expect(star(ctx([file("a")], { favorites: new Set(["a"]) }))).toBe("Unstar");
  });

  it("keeps menu-only actions out of the toolbar", () => {
    expect(ids(ctx([file("a")]), "toolbar")).not.toContain("open");
    expect(ids(ctx([]), "toolbar")).not.toContain("selectAll");
  });
});
