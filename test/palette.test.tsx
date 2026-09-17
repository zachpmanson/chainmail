// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { Palette } from "../src/components/Palette";
import { PALETTE, readPalette } from "../src/lib/palette";

/**
 * The values the table prints come out of the browser's own resolution of the
 * stylesheet, which jsdom does not do (vitest imports the .css files as
 * nothing). So the resolution is stood in for here: a stylesheet that answers
 * with the property's name and the theme in force, which is enough to tell
 * whether the table asked the right element, in the right theme, in the right
 * order — the parts of this that are ours.
 */
const styleFor = (el: Element) => ({
  getPropertyValue: (prop: string) => `${prop}:${el.getAttribute("data-theme") ?? "unset"}`,
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  document.documentElement.removeAttribute("data-theme");
});

it("reads both themes, and leaves the root in the theme it found", () => {
  vi.stubGlobal("getComputedStyle", styleFor);
  const root = document.documentElement;

  // Nothing set: the page is following the system, and the read has no business
  // leaving either theme behind on it.
  const following = readPalette(root);
  expect(following.light.bg).toBe("--bg:light");
  expect(following.dark.bg).toBe("--bg:dark");
  expect(root.getAttribute("data-theme")).toBeNull();

  // An explicit theme is the reader's, and is put back rather than left on
  // whichever of the two was read last.
  root.setAttribute("data-theme", "light");
  readPalette(root);
  expect(root.getAttribute("data-theme")).toBe("light");
});

it("asks for every colour in the palette", () => {
  const asked: string[] = [];
  vi.stubGlobal("getComputedStyle", (el: Element) => ({
    getPropertyValue: (prop: string) => {
      asked.push(prop);
      return styleFor(el).getPropertyValue(prop);
    },
  }));

  readPalette();

  // Every colour, in each theme: a colour listed in the data but never resolved
  // would be a row that could only ever print a dash.
  const names = PALETTE.map((c) => `--${c.name}`);
  expect(asked).toEqual([...names, ...names]);
});

it("shows every colour, both themes, and what each is for", () => {
  vi.stubGlobal("getComputedStyle", styleFor);
  render(<Palette />);

  // The header row, then one row per colour.
  expect(screen.getAllByRole("row")).toHaveLength(PALETTE.length + 1);
  for (const { name, what } of PALETTE) {
    const row = screen.getByText(`--${name}`).closest("tr")!;
    // Both themes' values are in the row, as the stylesheet resolved them: the
    // point of the table is that half of it is the theme that is not on.
    expect(row.textContent).toContain(`--${name}:light`);
    expect(row.textContent).toContain(`--${name}:dark`);
    expect(row.textContent).toContain(what);
  }
});

it("prints a dash rather than a colour the stylesheet does not define", () => {
  vi.stubGlobal("getComputedStyle", (el: Element) => ({
    getPropertyValue: () => (el.getAttribute("data-theme") === "light" ? "#f6f4f1" : ""),
  }));
  render(<Palette />);

  // The light column resolved; the dark one, which the stylesheet has nothing
  // for, is left empty rather than filled in with the light value.
  expect(screen.getAllByText("#f6f4f1")).toHaveLength(PALETTE.length);
  expect(screen.getAllByText("—")).toHaveLength(PALETTE.length);
});
