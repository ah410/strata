import { describe, expect, it } from "vitest";
import { bytes, colName, freeCopyName, joinPath, parentOf } from "./format";

describe("bytes", () => {
  it("keeps decimals only where they matter", () => {
    expect(bytes(0)).toBe("0 B");
    expect(bytes(512)).toBe("512 B");
    expect(bytes(1536)).toBe("1.50 KB");
    expect(bytes(15 * 1024 * 1024)).toBe("15.0 MB");
    expect(bytes(150 * 1024 ** 3)).toBe("150 GB");
  });
});

describe("paths", () => {
  it("joins and splits root-relative paths", () => {
    expect(joinPath("", "a")).toBe("a");
    expect(joinPath("a/b", "c")).toBe("a/b/c");
    expect(parentOf("a/b/c")).toBe("a/b");
    expect(parentOf("a")).toBe("");
  });
});

describe("colName", () => {
  it("maps column numbers to letters", () => {
    expect(colName(1)).toBe("A");
    expect(colName(26)).toBe("Z");
    expect(colName(27)).toBe("AA");
  });
});

describe("freeCopyName", () => {
  it("keeps a free name and numbers copies before the extension", () => {
    expect(freeCopyName("a.txt", new Set())).toBe("a.txt");
    expect(freeCopyName("a.txt", new Set(["a.txt"]))).toBe("a (copy).txt");
    expect(freeCopyName("a.txt", new Set(["a.txt", "a (copy).txt"]))).toBe("a (copy 2).txt");
  });

  it("treats folder names and dotfiles as having no extension", () => {
    expect(freeCopyName("v1.2", new Set(["v1.2"]), true)).toBe("v1.2 (copy)");
    expect(freeCopyName(".env", new Set([".env"]))).toBe(".env (copy)");
  });
});
