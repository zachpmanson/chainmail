import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

/**
 * Ask the corpus again for what is on this page, from the nav.
 *
 * Nothing in the app refetches on its own: the corpus changes only when an
 * operator ingests, and the query client holds every answer for five minutes (see
 * lib/queryClient) — the right default for reading, and the wrong one the moment
 * a person knows something has changed. So the one control that says "now" is
 * theirs, and it says it for everything: the list, the page, the people, the
 * folder list, and the deploy stamp beside it — which is how a merge that has just
 * gone live shows up without a reload.
 *
 * The icon is dripfeed-web's: the ↻ glyph its header spins while syncing, at the
 * same size and the same 0.8s linear turn. The spinner *is* the button's icon
 * rather than an overlay on it, so the control reads as one thing that is working
 * instead of a glyph swapped for a second glyph.
 *
 * Shut it stays a quiet glyph in the nav's small print, and it never says
 * anything about what it found: a refresh that reported "no changes" would be
 * answering a question nobody asked, on the row where the answer to "is my
 * merge live" is already written.
 */
export function NavRefresh() {
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);

  const refresh = async () => {
    setBusy(true);
    try {
      // Every query, not the ones this component can see: the nav is on every
      // page, and refreshing is one gesture a reader makes about the app rather
      // than about the panel that happens to be asking. Active queries refetch
      // now; inactive ones are marked stale and refetch when they are next needed.
      await qc.invalidateQueries();
    } finally {
      setBusy(false);
    }
  };

  return (
    <button
      type="button"
      className={busy ? "navrefresh busy" : "navrefresh"}
      // The word, for anyone reading the nav without seeing it; the glyph and the
      // turn are what a person sees, and neither needs a label beside it in a row
      // that is already small print.
      aria-label="Refresh"
      title="Refresh — ask the corpus again for what is on this page"
      // The turn is the whole of what says the work is happening, so it is also
      // said where no eyes are on it.
      aria-busy={busy}
      disabled={busy}
      onClick={() => void refresh()}
    >
      <span className="spinner" aria-hidden="true" />
    </button>
  );
}
