import { describe, expect, it } from "vitest";
import { combo, display, step, typeahead } from "./keys";

const k = (key: string, o: Partial<{ code: string; ctrlKey: boolean; metaKey: boolean; altKey: boolean; shiftKey: boolean }> = {}) =>
  ({ key, ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...o });

describe("combo", () => {
  it("maps Ctrl elsewhere and Cmd on a Mac to Mod", () => {
    expect(combo(k("c", { ctrlKey: true, code: "KeyC" }), false)).toBe("Mod+C");
    expect(combo(k("c", { metaKey: true, code: "KeyC" }), true)).toBe("Mod+C");
    expect(combo(k("c", { ctrlKey: true, code: "KeyC" }), true)).toBe("C");
  });

  it("reads letters from the physical key under Option", () => {
    expect(combo(k("˜", { altKey: true, code: "KeyN" }), true)).toBe("Alt+N");
  });

  it("keeps Shift for named keys but not for characters", () => {
    expect(combo(k("ArrowDown", { shiftKey: true }))).toBe("Shift+ArrowDown");
    expect(combo(k("?", { shiftKey: true, code: "Slash" }))).toBe("?");
    expect(combo(k(" "))).toBe("Space");
    expect(combo(k("F10", { shiftKey: true }))).toBe("Shift+F10");
  });
});

describe("display", () => {
  it("prints per platform", () => {
    expect(display("Mod+C", true)).toBe("⌘C");
    expect(display("Mod+C", false)).toBe("Ctrl+C");
    expect(display("Alt+ArrowUp", false)).toBe("Alt+↑");
    expect(display("Backspace", false)).toBe("Backspace");
    expect(display("Backspace", true)).toBe("⌫");
  });
});

describe("step", () => {
  it("moves in a list", () => {
    expect(step(2, "ArrowDown", 5, 1)).toBe(3);
    expect(step(4, "ArrowDown", 5, 1)).toBe(4);
    expect(step(0, "ArrowUp", 5, 1)).toBe(0);
    expect(step(2, "ArrowLeft", 5, 1)).toBe(2);
  });

  it("moves by rows in a grid", () => {
    expect(step(1, "ArrowDown", 10, 4)).toBe(5);
    expect(step(5, "ArrowUp", 10, 4)).toBe(1);
    expect(step(5, "ArrowRight", 10, 4)).toBe(6);
    expect(step(8, "ArrowDown", 10, 4)).toBe(9);
  });

  it("starts at the top with no cursor and jumps to the ends", () => {
    expect(step(-1, "ArrowDown", 5, 1)).toBe(0);
    expect(step(2, "End", 5, 1)).toBe(4);
    expect(step(2, "Home", 5, 1)).toBe(0);
  });
});

describe("typeahead", () => {
  const names = ["Archive", "budget.xlsx", "Books", "notes.md"];
  it("finds the next name with the prefix and wraps", () => {
    expect(typeahead(names, "b")).toBe(1);
    expect(typeahead(names, "b", 2)).toBe(2);
    expect(typeahead(names, "BO")).toBe(2);
    expect(typeahead(names, "a", 1)).toBe(0);
    expect(typeahead(names, "z")).toBe(-1);
  });
});
