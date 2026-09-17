import { useEffect } from "react";

/**
 * Escape drops a list's ticks.
 *
 * The ticks are what a reader builds up by clicking rows, and the build bar's
 * Deselect all is the only way out of them — a control that appears with the
 * selection and has to be found by eye. Escape is what everything else on the
 * page already answers: the search box shuts on it, the folder menu closes on it,
 * a reader who has ticked six chains out of a hundred expects the same key to
 * undo that.
 *
 * Nothing is listened for while nothing is ticked: Escape is not this page's key,
 * it is the key whatever is open on the page answers, and a handler that ran
 * anyway would be a page taking a key to do nothing with it.
 *
 * The search box and the folder menu are skipped, because they answer Escape
 * themselves by closing. Without that, a reader who had ticked rows and then
 * typed a query would lose the selection the moment they changed their mind
 * about the search — two things undone by one press, and only one of them asked
 * for.
 *
 * Shared by the two pages that hold a selection (the inbox and the search
 * results): they are the same list with the same bar over it, and the key cannot
 * mean one thing on one and something else on the other.
 */
export function useEscapeToClear(ticked: boolean, clear: () => void): void {
  useEffect(() => {
    if (!ticked) return;
    const esc = (ev: KeyboardEvent) => {
      if (ev.key !== "Escape") return;
      const at = ev.target as HTMLElement | null;
      // `.closest` on an element that is not one (a text node's parent is, the
      // document itself is not) — asked rather than assumed, since a keydown can
      // land on either.
      if (at?.closest?.(".navsearch, .ibfolders")) return;
      clear();
    };
    document.addEventListener("keydown", esc);
    return () => document.removeEventListener("keydown", esc);
  }, [ticked, clear]);
}
