import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError, $api } from "../lib/api";

/**
 * Ask the mailbox for what has arrived, and the corpus again for what is on this
 * page — the nav's one control that says "now".
 *
 * The corpus is re-read on its own now (see AutoRefresh): a tick while the tab is
 * visible and a read when the window comes back, which is enough to keep the page
 * from going stale under a reader who is not touching it. What that cadence
 * deliberately does not do is fetch the mailbox — mail arrives at the mailbox and
 * not in the corpus, and the ingest behind it is built to run hourly — so saying
 * "now" about what has ARRIVED is still the reader's to say: the one control that
 * says it is the press, and a press means fetching before it means reading.
 *
 * So the press is the page's own refresh in two halves, and now both of them:
 * `POST /v1/slurp` first, which is the ingest the hourly sweep runs (mail, twins,
 * repair, the unread reconciliation and the embedder), then the queries are
 * invalidated, so the list, the page, the people, the folders and the deploy stamp
 * are all asked again over a corpus that holds what was just fetched. Fetch, then
 * read, in that order: a refetch first would answer with the corpus as it was and
 * leave the new mail invisible until the next press.
 *
 * The page's own refresh button has done exactly this since it was added (see
 * ViewPage, "web: the refresh button slurps before it re-derives"), and the nav's
 * glyph did not — which made the two look like the same gesture and be different
 * ones, in the direction that wastes a press: the page button fetched, the nav
 * button only re-read, and the reader had no way to tell which of the two they had
 * pressed.
 *
 * **A slurp that is refused is not a failed refresh.** Without `-slurp` the
 * endpoint answers 403 and this stays what it always was: a re-read of the corpus
 * on a host that cannot reach the mailbox. So the refusal is logged and the
 * queries are invalidated anyway — the read half is always safe and always the
 * point of the button. The same is true of a 409, which is the one-at-a-time latch
 * saying an ingest is already walking the mailbox: the work is being done, and the
 * refetch reads the corpus it is writing into.
 *
 * The transcript the ingest answers with, and the refusals, go to the console
 * rather than onto the page, for the reason the page's own button does it: this
 * control is on every page and belongs to none of them, and a report of what has
 * ARRIVED is news a reader did not ask for on the row where "is my merge live" is
 * already written. What a failure must not be is silent *and* invisible — a press
 * that spends mailbox round trips stays a console line here rather than a panel
 * over somebody else's page (see the toast channel the mailbox writes use).
 *
 * The icon is dripfeed-web's: the ↻ glyph its header spins while syncing, at the
 * same size and the same 0.8s linear turn. The spinner *is* the button's icon
 * rather than an overlay on it, so the control reads as one thing that is working
 * instead of a glyph swapped for a second glyph. It turns for the whole of the
 * press now, ingest included — an ingest is minutes on a cold corpus, and the turn
 * is what says the button has not simply been missed.
 */
export function NavRefresh() {
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);

  // The ingest, through the same endpoint and the same optional grant the page's
  // own refresh button uses. Nothing here names phases or a query: which pipeline
  // a person-pressed slurp runs is the server's `manualPhases`, and a second
  // caller spelling it out would be a second answer to that question.
  const slurp = $api.useMutation("post", "/v1/slurp", {
    onSuccess: (data) => console.log(data.report?.trim() || "slurp: nothing to report"),
    onError: (e) =>
      console.error(
        e instanceof ApiError && e.status === 403
          ? "no mailbox reach on this host (the server was started without -slurp), so this re-reads what the corpus already holds"
          : e instanceof ApiError && e.status === 409
            ? "a sweep is already running: the mailbox is being ingested right now, and this refetch reads the corpus it is writing into"
            : `slurp failed: ${e instanceof Error ? e.message : String(e)}`,
      ),
  });

  const refresh = async () => {
    setBusy(true);
    try {
      // Caught rather than allowed to escape: the read half below is not
      // conditional on the fetch, and the message for each way it can fail is
      // already written in the hook above.
      await slurp.mutateAsync({}).catch(() => {});
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
      title="Refresh — fetch what has arrived, then ask the corpus again for what is on this page"
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
