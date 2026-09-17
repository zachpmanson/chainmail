/**
 * The theme's colours, as data, so the settings page can show them.
 *
 * The palette itself lives in styles.css and only there — these are the names
 * of the custom properties it defines, and nothing here repeats a value. Every
 * value the table prints is read back out of the stylesheet, which is what
 * makes the table worth having while debugging: a colour that is listed here
 * but resolves to nothing, or resolves to something the other theme does not,
 * is exactly the kind of drift a hand-written copy of the palette would hide.
 *
 * Order is the order the variables are declared in styles.css: the page's own
 * furniture first, then the reader's mail, then the participant colours.
 */
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
 * Both themes at once, read from the live stylesheet.
 *
 * The two sets of declarations are steered by a media query and an attribute
 * selector, so the only way to ask what the theme that is *not* on resolves to
 * is to put it on the root, read, and put the root back. The three reads happen
 * inside one synchronous call, so nothing paints in between and the reader
 * never sees a theme they did not ask for.
 *
 * "light" and "dark" are written onto the root rather than cleared to get the
 * light values: clearing the attribute means "follow the system", which is a
 * third answer that happens to be the same as one of these two today.
 *
 * A value the browser cannot resolve comes back as an empty string, and is left
 * that way rather than guessed at: a table that filled in a colour the
 * stylesheet does not define would be the one thing it must never do.
 */
export function readPalette(root: HTMLElement = document.documentElement): Palette {
  const was = root.getAttribute("data-theme");
  const light = readTheme(root, "light");
  const dark = readTheme(root, "dark");
  if (was === null) root.removeAttribute("data-theme");
  else root.setAttribute("data-theme", was);
  return { light, dark };
}
