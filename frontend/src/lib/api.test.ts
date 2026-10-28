import { describe, expect, it } from "vitest";
import { errorText } from "./api";

describe("errorText", () => {
  it("keeps plain server messages", () => {
    expect(errorText(400, "invalid file name\n")).toBe("invalid file name");
  });

  it("turns a proxy's HTML 413 page into a sentence", () => {
    const html = "<html> <head><title>413 Request Entity Too Large</title></head><body>openresty</body></html>";
    expect(errorText(413, html)).toMatch(/reverse proxy.*too large/);
  });

  it("never shows markup for other proxy errors", () => {
    expect(errorText(502, "<html><body>Bad Gateway</body></html>")).not.toMatch(/</);
    expect(errorText(500, "<!DOCTYPE html><html></html>")).not.toMatch(/</);
  });

  it("falls back when the body is empty", () => {
    expect(errorText(500, "", "Upload failed")).toBe("Upload failed");
  });
});
