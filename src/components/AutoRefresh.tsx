import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

/**
 * The corpus, re-read on its own: a tick every ninety seconds while the tab is in
 * front of the reader, and one the moment the window comes back to it.
 *
 * The app has two ways to be made current and this is the second. The press (see
 * NavRefresh) says "now" about the mailbox: `POST /v1/slurp` walks the inbox —
 * mail, twins, repair, the unread reconciliation, the embedder — and then the
 * queries are asked again over the corpus it has just written. A tick is only the
 * second half of that, the read, and never the first. The line is drawn there
 * because the two halves cost nothing alike: a slurp holds a one-at-a-time latch
 * that answers 409 to a second caller and is built to run hourly, so a client
 * firing it on a cadence would stack ingests against a service that is
 * deliberately slow, while a read is a question the corpus can be asked as often
 * as a reader looks at it.
 *
 * Said plainly, because it is the thing a reader will notice: mail appears on a
 * tick only once something else has ingested it (the hourly sweep), and the press
 * stays what it always was — the way to go and look at the mailbox right now.
 * Ninety seconds is how stale the corpus may be for a reader who is not pressing
 * anything, which is the question this control answers.
 *
 * Focus is the half the reader actually notices, so it is not left to the next
 * tick: coming back to the window is the moment they want to see what is new, and
 * one refetch is owed then rather than after the remainder of the interval. A
 * hidden tab does not tick at all — the timer is stopped rather than left to fire
 * against a page nobody is reading — and coming back restarts it and reads once.
 *
 * The rest is what keeps a cadence from becoming a stampede:
 *
 *   - One read at a time. A tick that is still in flight is not a reason to start
 *     another, and neither is a focus that arrives mid-read. `invalidateQueries`
 *     is asked with `cancelRefetch: false` as well, so it cannot cancel a fetch
 *     that some other part of the app already had in the air and begin a second.
 *   - Focus and visibility are two signals about one event. A browser reports
 *     both when a window comes back, so a read that follows one within the last
 *     second is taken as the same event: the pair becomes one refetch, not two.
 *   - Nothing here fetches the mailbox, so the refusals the press has to explain
 *     (403 on a host started without `-slurp`, 409 while a sweep runs) cannot
 *     happen on a cadence and cannot repeat onto the console every ninety
 *     seconds. A read that fails is left to the client's own retry policy, which
 *     already knows what this service's statuses mean (see lib/queryClient).
 *
 * A behaviour rather than a view: it draws nothing and belongs to the shell, so
 * the cadence holds on every page without any page asking for it.
 */

/** How often a visible page asks the corpus again. */
export const AUTO_REFRESH_MS = 90_000;

/**
 * Two wake signals this close together are one event. Coming back to a window is
 * both a `focus` and a `visibilitychange`, in whichever order the browser chose,
 * and a read for each would be the same read twice. A second is far longer than
 * the gap between the two events and far shorter than the cadence.
 */
const ONE_WAKE_MS = 1000;

export function AutoRefresh() {
  const qc = useQueryClient();

  useEffect(() => {
    let ticker: ReturnType<typeof setInterval> | null = null;
    let reading = false;
    let lastWakeAt = -Infinity;

    // The read half of the refresh and nothing else. `cancelRefetch: false` is
    // the no-overlap rule stated to the query client itself — a query already in
    // the air is left to finish rather than cancelled and begun again — and the
    // `reading` flag is the same rule for this component's own reads.
    const read = async () => {
      if (reading || document.visibilityState === "hidden") return;
      reading = true;
      try {
        await qc.invalidateQueries({}, { cancelRefetch: false });
      } finally {
        reading = false;
      }
    };

    const wake = () => {
      const now = Date.now();
      if (now - lastWakeAt < ONE_WAKE_MS) return;
      lastWakeAt = now;
      void read();
    };

    const start = () => {
      if (ticker === null) ticker = setInterval(() => void read(), AUTO_REFRESH_MS);
    };
    const stop = () => {
      if (ticker !== null) {
        clearInterval(ticker);
        ticker = null;
      }
    };

    // Hiding pauses; showing resumes and reads once. The interval is not left
    // running behind a hidden tab just to have every tick return early — nothing
    // is scheduled while there is nobody to read for.
    const onVisibility = () => {
      if (document.visibilityState === "hidden") {
        stop();
        return;
      }
      start();
      wake();
    };

    if (document.visibilityState !== "hidden") start();
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("focus", wake);

    return () => {
      stop();
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("focus", wake);
    };
  }, [qc]);

  return null;
}
