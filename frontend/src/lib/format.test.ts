import { describe, expect, it } from "vitest";
import { bytes, colName, joinPath, parentOf } from "./format";

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
