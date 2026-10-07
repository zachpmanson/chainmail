/** Custom property names only; values are read back from styles.css, in its declaration order. */
export interface PaletteColour {
  /** the custom property's name, without the leading dashes */
  name: string;
  /** what the colour is for, in the words a reader of a page would use */
  what: string;
}

export const PALETTE: PaletteColour[] = [
  { name: "bg", what: "the page" },
  { name: "fg", what: "text" },
  { name: "muted", what: "notes and secondary text" },
  { name: "line", what: "borders and rules" },
  { name: "card", what: "surfaces that sit on the page" },
  { name: "accent", what: "links, and what hovers" },
  { name: "mine", what: "the reader's own messages" },
  { name: "quote", what: "quoted mail" },
  { name: "dash", what: "a bubble drawn as a dashed one" },
  { name: "o1", what: "participant 1" },
  { name: "o2", what: "participant 2" },
  { name: "o3", what: "participant 3" },
  { name: "o4", what: "participant 4" },
  { name: "o5", what: "participant 5" },
  { name: "strong", what: "an agreeing similarity figure" },
];

/** What one theme resolves each colour to, keyed by the property's name. */
export type PaletteReadings = Record<string, string>;

export interface Palette {
  light: PaletteReadings;
  dark: PaletteReadings;
}

/** One theme's values, as the browser resolves them for a root element. */
function readTheme(root: Element, theme: Theme): PaletteReadings {
  root.setAttribute("data-theme", theme);
  const declared = getComputedStyle(root);
  const readings: PaletteReadings = {};
  for (const { name } of PALETTE) {
    readings[name] = declared.getPropertyValue(`--${name}`).trim();
  }
  return readings;
}

type Theme = "light" | "dark";

/**
 * Reads the inactive theme by setting data-theme on the root and restoring it, all
 * synchronously so nothing paints. Unresolvable values stay "" rather than guessed.
 */
export function readPalette(root: HTMLElement = document.documentElement): Palette {
  const was = root.getAttribute("data-theme");
  const light = readTheme(root, "light");
  const dark = readTheme(root, "dark");
  if (was === null) root.removeAttribute("data-theme");
  else root.setAttribute("data-theme", was);
  return { light, dark };
}
