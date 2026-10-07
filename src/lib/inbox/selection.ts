import { useEffect } from "react";

/** Skips the search box and folder menu, which answer Escape themselves by closing. */
export function useEscapeToClear(ticked: boolean, clear: () => void): void {
  useEffect(() => {
    if (!ticked) return;
    const esc = (ev: KeyboardEvent) => {
      if (ev.key !== "Escape") return;
      const at = ev.target as HTMLElement | null;
      // The keydown target may be the document, which has no .closest.
      if (at?.closest?.(".navsearch, [data-folders]")) return;
      clear();
    };
    document.addEventListener("keydown", esc);
    return () => document.removeEventListener("keydown", esc);
  }, [ticked, clear]);
}
