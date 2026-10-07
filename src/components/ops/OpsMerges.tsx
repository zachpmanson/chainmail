import { $api } from "../../lib/api/api";
import { useMergeRun } from "../../lib/ops/opsMerges";
import Checkbox from "../ui/Checkbox";
import Button from "../ui/Button";
import InlineAlert from "../ui/InlineAlert";
import MergeCard from "./MergeCard";
import MergeConfirm from "./MergeConfirm";
import MergePlanDetails from "./MergePlanDetails";
import { errText } from "../../lib/ui/errText";

export default function OpsMerges() {
  const plan = $api.useQuery("get", "/v1/ops/plan", {});
  const data = plan.data;
  const run = useMergeRun(data?.merges);
  const { phase, busy, selected, merges, applicable, chosen, allPicked } = run;

  return (
    <>
      {plan.isError ? <InlineAlert>{errText(plan.error)}</InlineAlert> : null}
      {run.error ? <InlineAlert>{run.error}</InlineAlert> : null}
      {run.last ? (
        <p className="my-1.5 mb-2 text-xs text-muted">
          {run.last} — the plan below is the current one.
        </p>
      ) : null}

      <h2 className="mt-4 mb-0.5 text-xs tracking-[.1em] text-muted uppercase">Merge plan</h2>
      {applicable.length > 0 ? (
        <div className="mt-2 flex items-center justify-between gap-2 rounded-lg border border-line bg-quote px-3 py-1.5">
          <label className="flex cursor-pointer items-center gap-2 text-xs text-fg">
            <Checkbox
              checked={allPicked}
              disabled={busy}
              ref={(el) => {
                if (el) el.indeterminate = selected.size > 0 && !allPicked;
              }}
              onChange={(e) => run.pickAll(e.target.checked)}
            />
            select all {applicable.length} applicable
          </label>
          <Button
            type="button"
            variant="primary"
            density="compact"
            disabled={selected.size === 0 || busy}
            onClick={run.confirm}
          >
            {phase.kind === "merging"
              ? `merging ${phase.progress ?? ""}`
              : `merge ${selected.size} selected`}
          </Button>
        </div>
      ) : null}
      {phase.kind === "confirming" && chosen.length > 0 ? (
        <MergeConfirm chosen={chosen} onConfirm={() => void run.run()} onCancel={run.cancel} />
      ) : null}
      {!data ? (
        <p className="my-1.5 mb-2 text-xs text-muted">
          {plan.isPending ? "Reading the plan…" : "No plan."}
        </p>
      ) : merges.length === 0 ? (
        <p className="my-1.5 mb-2 text-xs text-muted">
          Nothing to merge — every name-only person the pass found has been folded, and no other
          tier is applicable here.
        </p>
      ) : (
        <ol className="mt-2 list-none p-0">
          {merges.map((m) => (
            <li
              key={m.dropId}
              className="rounded-lg border border-line bg-card px-3 py-2 not-first:mt-2"
            >
              <MergeCard
                m={m}
                selected={selected.has(m.dropId)}
                busy={busy}
                onSelect={(picked) => run.pick(m.dropId, picked)}
              />
            </li>
          ))}
        </ol>
      )}

      {data ? <MergePlanDetails data={data} /> : null}
    </>
  );
}
