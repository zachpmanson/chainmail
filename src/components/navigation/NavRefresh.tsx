import { useState } from "react";
import { ArrowPathIcon } from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError, $api } from "../../lib/api/api";
import { when } from "../../lib/ui/stamp";
import IconButton from "../ui/IconButton";

/**
 * Slurps the mailbox, then invalidates every query; fetch first or the refetch misses new mail.
 * A 403 (no -slurp) or 409 (ingest running) still refetches; the read half is always safe.
 */
export default function NavRefresh() {
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const status = $api.useQuery("get", "/v1/status", {}, { refetchInterval: 10_000 });
  const sweep = status.data?.sweep;
  const running = Boolean(sweep?.running);
  const warning = !running && (sweep?.outcome === "incomplete" || sweep?.outcome === "failed");
  const indicator = busy || running;
  const stageLabel = running
    ? "ingesting"
    : warning
      ? sweep?.outcome === "incomplete"
        ? "ingest incomplete"
        : "ingest failed"
      : null;
  const label = running
    ? `Refresh; ingesting mail${sweep?.startedAt ? `, started ${when(sweep.startedAt)}` : ""}`
    : warning
      ? sweep?.outcome === "incomplete"
        ? "Refresh; last ingest stopped early"
        : "Refresh; last ingest failed"
      : "Refresh";
  const title = running
    ? `Ingesting mail${sweep?.startedAt ? `, started ${when(sweep.startedAt)}` : ""}. Threads fill in as the walk runs.`
    : warning
      ? sweep?.outcome === "incomplete"
        ? `The last ingest stopped early${sweep.finishedAt ? ` (${when(sweep.finishedAt)})` : ""}: a phase hit its bound, so the corpus is missing mail it was asked for. Ingesting again continues from where it stopped.`
        : `The last ingest failed${sweep?.finishedAt ? ` (${when(sweep.finishedAt)})` : ""}: a phase broke, so the corpus is only as far as that phase got. See the journal.`
      : "Refresh — fetch what has arrived, then ask the corpus again for what is on this page";

  // Phases are the server's choice (`manualPhases`); don't name them here.
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
      // The refetch runs regardless; failures are logged by the hook above.
      await slurp.mutateAsync({}).catch(() => {});
      await qc.invalidateQueries();
    } finally {
      setBusy(false);
    }
  };

  return (
    <span className="inline-flex shrink-0 items-center gap-1.5">
      <IconButton
        aria-label={label}
        title={title}
        aria-busy={indicator}
        disabled={busy}
        onClick={() => void refresh()}
      >
        <ArrowPathIcon
          className={[indicator && "motion-safe:animate-spin", warning && "text-accent"]
            .filter(Boolean)
            .join(" ")}
          aria-hidden="true"
        />
      </IconButton>
      {stageLabel && <span className="whitespace-nowrap text-xs">{stageLabel}</span>}
    </span>
  );
}
