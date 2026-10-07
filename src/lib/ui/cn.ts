import { extendTailwindMerge, fromTheme, validators } from "tailwind-merge";

// Mirrors the @theme tokens in src/tailwind.css.
const twMerge = extendTailwindMerge({
  extend: {
    theme: {
      text: ["2xs"],
      color: [
        "bg",
        "fg",
        "muted",
        "line",
        "card",
        "accent",
        "mine",
        "quote",
        "dash",
        "strong",
        "org-1",
        "org-2",
        "org-3",
        "org-4",
        "org-5",
      ],
    },
    classGroups: {
      "font-family": [{ font: [(v: string) => v === "[inherit]"] }],
    },
  },
  override: {
    classGroups: {
      // Stock config reads font-[inherit] as a weight, so font-semibold would strip the reset.
      "font-weight": [
        {
          font: [
            fromTheme("font-weight"),
            validators.isArbitraryVariableWeight,
            (v: string) => v !== "[inherit]" && validators.isArbitraryWeight(v),
          ],
        },
      ],
    },
  },
});

/** Joins class lists; a later class beats a conflicting earlier one whatever Tailwind's CSS order. */
export function cn(...classes: (string | false | null | undefined)[]): string {
  return twMerge(...classes);
}
