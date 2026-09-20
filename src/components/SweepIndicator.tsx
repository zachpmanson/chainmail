import { $api } from "../lib/api";
import { when } from "../lib/stamp";

/**
 * The ingest the page did not start, said out loud.
 *
 * The nav's ↻ spins while *your* press runs and is still when it is not — right
 * for a button, and silent about the walk nobody pressed. The cadence sweeps the
 * mailbox on its own (see sweepLoop), and a rebuild is minutes long; during those
 * minutes the corpus is genuinely half-built. Mailbox parents are linked per
 * batch of the walk (see mailingest), so until the walk ends a fresh corpus has
 * almost no reply spine, and every message that quotes another looks like a
 * thread root. That is indistinguishable from a threading bug on the page, which
 * is how it was read when the corpus was rebuilt on 2026-09-20 — the work was
 * happening and nothing said so.
 *
 * So this is the one thing the nav has to say that is not a control: while a
 * sweep is in flight, that it is, and when it started. And then the state that
 * outlives it — an ingest that ended INCOMPLETE (a phase stopped at a bound, so
 * the corpus is short of what the walk was asked for) or FAILED. The second is
 * the one a reader most needs: the corpus is whole enough to read and missing
 * mail, and nothing on a page built over it would say so. It clears when the next
 * run ends complete, because the state it reports is the last run's, not a
 * backlog of them.
 *
 * Both are read off GET /v1/status, which already answers the connection question
 * (see SettingsView) — one route for "is the machine well", polled only while
 * something could change: TanStack's refetchInterval keeps asking while the tab is
 * awake, and the query client's five-minute staleness is what suits a read, not a
 * fact with a lifetime of seconds.
 *
 * A silent failure is the shape this file avoids: if the route cannot be reached,
 * nothing renders, the same as "nothing is happening".
 */
export function SweepIndicator() {
  const status = $api.useQuery(
    "get",
    "/v1/status",
    {},
    // Ten seconds: short against a walk that runs for minutes, and cheap on a
    // loopback API whose answer is a file read and two fields.
    { refetchInterval: 10_000 },
  );
  const sweep = status.data?.sweep;
  if (!sweep) return null;

  if (sweep.running) {
    return (
      <span
        className="sweepind working"
        role="status"
        // Said where no eyes are on it, as the refresh button's own turn is.
        aria-label={`Ingesting mail${sweep.startedAt ? `, started ${when(sweep.startedAt)}` : ""}`}
        title={
          `Ingesting mail${sweep.startedAt ? `, started ${when(sweep.startedAt)}` : ""}. ` +
          "Threads fill in as the walk runs — a message that quotes another is only linked " +
          "to it once the batch that holds it is done."
        }
      >
        <span className="spinner" aria-hidden="true" />
        ingesting
      </span>
    );
  }

  if (sweep.outcome === "incomplete" || sweep.outcome === "failed") {
    const stopped = sweep.outcome === "incomplete";
    return (
      <span
        className="sweepind warn"
        role="status"
        aria-label={stopped ? "The last ingest stopped early" : "The last ingest failed"}
        title={
          stopped
            ? `The last ingest stopped early${sweep.finishedAt ? ` (${when(sweep.finishedAt)})` : ""}: ` +
              "a phase hit its bound, so the corpus is missing mail it was asked for. " +
              "Ingesting again continues from where it stopped."
            : `The last ingest failed${sweep.finishedAt ? ` (${when(sweep.finishedAt)})` : ""}: ` +
              "a phase broke, so the corpus is only as far as that phase got. See the journal."
        }
      >
        {stopped ? "ingest incomplete" : "ingest failed"}
      </span>
    );
  }

  return null;
}