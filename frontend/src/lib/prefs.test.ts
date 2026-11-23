import { describe, expect, it } from "vitest";
import { getPrefs, setPref } from "./prefs";

describe("prefs", () => {
  it("defaults previews off and remembers a change", () => {
    expect(getPrefs().previews).toBe(false);
    setPref("previews", true);
    expect(getPrefs().previews).toBe(true);
  });
});
