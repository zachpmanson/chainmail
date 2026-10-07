import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { $api, type OpsMerge, type OpsMergeRecord } from "../../lib/api/api";
import { applyMergeBatch } from "../../lib/ops/opsMerges";
import { when } from "../../lib/ui/stamp";
import Checkbox from "../ui/Checkbox";
import StatusBadge from "../ui/StatusBadge";
import { Button } from "../ui/controls";
import InlineAlert from "../ui/InlineAlert";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/** The dedupe rules, labelled for a screen the CLI naming would obscure. */
const RULES: Record<string, string> = {
  "dedupe:same-display-name": "same display name",
  "dedupe:same-display-name-in-thread": "same display name, in thread",
  "dedupe:first-name-and-org": "first name at one organisation",
  "dedupe:webmail-and-work-mailbox": "webmail and work mailbox",
};
const ruleLabel = (rule: string): string => RULES[rule] ?? rule;

/** The server decides applicability; the checkbox only mirrors it. */
function MergeCard({
  m,
  selected,
  busy,
  onSelect,
}: {
  m: OpsMerge;
  selected: boolean;
  busy: boolean;
  onSelect: (picked: boolean) => void;
}) {
  const keep =
    `#${m.keepId} ${m.keepName}` +
    (m.keepIdentities?.length ? ` · ${m.keepIdentities.join(", ")}` : "");
  const drop =
    `#${m.dropId} ${m.dropName}` +
    (m.dropIdentities?.length ? ` · ${m.dropIdentities.join(", ")}` : "");
  return (
    <article>
      <p className="mt-0 mb-1.5 text-[.68rem] font-bold uppercase tracking-[.05em] text-muted">
        {m.applicable ? (
          <Checkbox
            className="mr-2 align-middle cursor-pointer"
            checked={selected}
            disabled={busy}
            onChange={(e) => onSelect(e.target.checked)}
            aria-label={`Select folding #${m.dropId} ${m.dropName} into #${m.keepId} ${m.keepName}`}
          />
        ) : null}
        {ruleLabel(m.rule)}
        {m.applicable ? (
          <StatusBadge className="ml-2" tone="success">
            apply
          </StatusBadge>
        ) : (
          <StatusBadge tone="neutral">read-only</StatusBadge>
        )}
      </p>
      <p className="my-0.5 text-[.84rem] leading-[1.35] [&_code]:text-[.74rem] [&_code]:[overflow-wrap:anywhere]">
        keep&nbsp;<code>{keep}</code>
      </p>
      <p className="my-0.5 text-[.84rem] leading-[1.35] [&_code]:text-[.74rem] [&_code]:[overflow-wrap:anywhere]">
        drop&nbsp;<code>{drop}</code>
      </p>
      {m.evidence ? <p className="my-1 mb-2 text-[.74rem] text-muted">{m.evidence}.</p> : null}
    </article>
  );
}

function OneTrail(t: OpsMergeRecord) {
  return (
    <li className="py-1 text-[.8rem] leading-[1.5]">
      <span className="text-[.72rem] tabular-nums text-muted">{when(t.mergedAt)}</span>
      <code>#{t.keepId}</code> {t.keepName ?? ""} <span className="text-muted">←</span>{" "}
      <code>#{t.dropId}</code> {t.dropName ?? ""}
      {t.reason ? <span className="block text-[.72rem] text-muted">{t.reason}</span> : null}
    </li>
  );
}

export default function OpsMerges() {
  const qc = useQueryClient();
  const plan = $api.useQuery("get", "/v1/ops/plan", {});
  // Ticked pairs, by the id of the person they drop (unique in a plan).
  const [selected, setSelected] = useState<Set<number>>(new Set());
  // The ticked batch is past its first click, waiting on the confirm.
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [last, setLast] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState<string | null>(null);
  // Hides merged pairs until the plan refetch, which drops them, lands.
  const [applied, setApplied] = useState<Set<number>>(new Set());
  const apply = $api.useMutation("post", "/v1/ops/merge", {
    onError: (e) => setError(errText(e)),
  });

  const data = plan.data;
  // Applicable pairs first: they are what this screen exists to act on.
  const merges = data
    ? [...data.merges]
        .filter((m) => !applied.has(m.dropId))
        .sort((a, b) => Number(b.applicable) - Number(a.applicable))
    : [];
  const applicable = merges.filter((m) => m.applicable);
  const chosen = merges.filter((m) => selected.has(m.dropId));
  const allPicked = applicable.length > 0 && applicable.every((m) => selected.has(m.dropId));

  function pick(id: number, picked: boolean) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (picked) next.add(id);
      else next.delete(id);
      return next;
    });
  }

  /** Sequential on purpose: each POST re-derives the plan server-side, so a pair an
   *  earlier merge made moot comes back as a 409 and the rest are not sent. */
  async function mergeChosen() {
    const batch = chosen;
    setConfirming(false);
    setError(null);
    setBusy(true);
    const result = await applyMergeBatch(
      batch,
      (target) => apply.mutateAsync({ body: { keepId: target.keepId, dropId: target.dropId } }),
      setProgress,
      (dropId) => setApplied((prev) => new Set(prev).add(dropId)),
    );
    if (result.refusal) {
      const { target, error: refusal } = result.refusal;
      setError(
        `${result.merged} of ${result.total} merged, then folding #${target.dropId} into #${target.keepId} ` +
          `was refused: ${errText(refusal)}`,
      );
    }
    setSelected(new Set());
    setBusy(false);
    setProgress(null);
    if (result.merged > 0)
      setLast(result.merged === 1 ? "merged 1 pair" : `merged ${result.merged} pairs`);
    await qc.invalidateQueries({ queryKey: ["get", "/v1/ops/plan"] });
  }
  return (
    <>
      {plan.isError ? <InlineAlert>{errText(plan.error)}</InlineAlert> : null}
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      {last ? (
        <p className="my-1.5 mb-2 text-[.74rem] text-muted">
          {last} — the plan below is the current one.
        </p>
      ) : null}

      <h2 className="mt-4 mb-0.5 text-[.7rem] uppercase tracking-[.1em] text-muted">Merge plan</h2>
      {applicable.length > 0 ? (
        <div className="mt-2 flex items-center justify-between gap-2 rounded-[9px] border border-line bg-quote px-3 py-1.5">
          <label className="flex items-center gap-2 text-[.76rem] text-fg cursor-pointer">
            <Checkbox
              checked={allPicked}
              disabled={busy}
              ref={(el) => {
                if (el) el.indeterminate = selected.size > 0 && !allPicked;
              }}
              onChange={(e) =>
                setSelected(e.target.checked ? new Set(applicable.map((m) => m.dropId)) : new Set())
              }
            />
            select all {applicable.length} applicable
          </label>
          <Button
            type="button"
            variant="primary"
            density="compact"
            disabled={selected.size === 0 || busy}
            onClick={() => {
              setError(null);
              setConfirming(true);
            }}
          >
            {busy ? `merging ${progress ?? ""}` : `merge ${selected.size} selected`}
          </Button>
        </div>
      ) : null}
      {confirming && chosen.length > 0 ? (
        <div className="mt-2 mb-0.5 rounded-md border border-line bg-quote px-2 py-2 text-[.76rem] leading-[1.5] text-fg">
          <p className="m-0">
            <strong>This cannot be undone</strong> — a merge is recorded, never reversed.{" "}
            {chosen.length === 1 ? "This pair" : `These ${chosen.length} pairs`} will be folded into
            their keepers now:
          </p>
          <ul className="mt-1.5 mb-0 list-none pl-0.5">
            {chosen.map((m) => (
              <li
                key={m.dropId}
                className="py-px text-[.76rem] leading-[1.5] [&_code]:text-[.72rem] [&_code]:[overflow-wrap:anywhere]"
              >
                <code>
                  #{m.dropId} {m.dropName}
                </code>{" "}
                <span className="text-muted">→</span>{" "}
                <code>
                  #{m.keepId} {m.keepName}
                </code>
              </li>
            ))}
          </ul>
          <div className="mt-2 flex items-center gap-2">
            <Button
              type="button"
              variant="danger"
              density="compact"
              disabled={busy}
              onClick={mergeChosen}
            >
              {busy
                ? "merging…"
                : chosen.length === 1
                  ? "merge this pair"
                  : `merge these ${chosen.length} pairs`}
            </Button>
            <Button
              type="button"
              variant="secondary"
              density="compact"
              disabled={busy}
              onClick={() => setConfirming(false)}
            >
              cancel
            </Button>
          </div>
        </div>
      ) : null}
      {!data ? (
        <p className="my-1.5 mb-2 text-[.74rem] text-muted">
          {plan.isPending ? "Reading the plan…" : "No plan."}
        </p>
      ) : merges.length === 0 ? (
        <p className="my-1.5 mb-2 text-[.74rem] text-muted">
          Nothing to merge — every name-only person the pass found has been folded, and no other
          tier is applicable here.
        </p>
      ) : (
        <ol className="mt-2 list-none p-0">
          {merges.map((m) => (
            <li
              key={m.dropId}
              className="not-first:mt-2 rounded-[9px] border border-line bg-card px-3 py-2"
            >
              <MergeCard
                m={m}
                selected={selected.has(m.dropId)}
                busy={busy}
                onSelect={(picked) => pick(m.dropId, picked)}
              />
            </li>
          ))}
        </ol>
      )}

      {data ? (
        <>
          <details className="pan" open={data.refusals.length > 0}>
            <summary>
              {data.refusals.length} refusal{data.refusals.length === 1 ? "" : "s"} — shown, never
              applied
            </summary>
            <div>
              {data.refusals.length === 0 ? (
                <p className="my-1.5 mb-2 text-[.74rem] text-muted">None.</p>
              ) : (
                <ul className="mt-1 mb-0 list-none p-0 [&_li]:py-1 [&_li]:text-[.78rem] [&_li]:leading-[1.45] [&_li_code]:text-[.72rem]">
                  {data.refusals.map((r) => (
                    <li key={`${r.rule}:${r.subject}`}>
                      <span className="text-muted">{ruleLabel(r.rule)}</span>{" "}
                      <code>{r.subject}</code> — {r.reason}{" "}
                      <span className="text-muted">
                        (people {r.people.map((p) => `#${p}`).join(", ")})
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </details>

          <details className="pan">
            <summary>
              {data.candidates.length} candidate{data.candidates.length === 1 ? "" : "s"} for a
              human glance
            </summary>
            <div>
              {data.candidates.length === 0 ? (
                <p className="my-1.5 mb-2 text-[.74rem] text-muted">None.</p>
              ) : (
                <ul className="mt-1 mb-0 list-none p-0 [&_li]:py-1 [&_li]:text-[.78rem] [&_li]:leading-[1.45] [&_li_code]:text-[.72rem]">
                  {data.candidates.map((c) => (
                    <li key={`${c.aId}:${c.bId}`}>
                      <code>{c.aName}</code> ~ <code>{c.bName}</code> — {c.reason}
                      {c.suggest ? (
                        <>
                          {" "}
                          <code>{c.suggest}</code>
                        </>
                      ) : null}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </details>

          <details className="pan">
            <summary>
              {data.twinsDeclined.length === 0
                ? "the twins pass declined nothing"
                : `twins pass declined ${data.twinsDeclined.reduce((n, d) => n + d.count, 0)} entries`}
            </summary>
            <div>
              {data.twinsDeclined.length === 0 ? (
                <p className="my-1.5 mb-2 text-[.74rem] text-muted">
                  None — every stored copy collapsed.
                </p>
              ) : (
                <ul className="mt-1 mb-0 list-none p-0 [&_li]:py-1 [&_li]:text-[.78rem] [&_li]:leading-[1.45] [&_li_code]:text-[.72rem]">
                  {data.twinsDeclined.map((d) => (
                    <li key={d.reason}>
                      <span className="text-muted">{d.count}</span> — {d.reason}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </details>

          <details className="pan" open>
            <summary>
              {data.trail.length === 1 ? "1 merge" : `${data.trail.length} merges`} recorded — the
              person_merges trail
            </summary>
            <div>
              {data.trail.length === 0 ? (
                <p className="my-1.5 mb-2 text-[.74rem] text-muted">
                  None yet. The trail is the audit record of every merge, by whatever surface it was
                  made.
                </p>
              ) : (
                <ul className="mt-1 mb-0 list-none p-0 [&_li]:py-1 [&_li]:text-[.8rem] [&_li]:leading-[1.5]">
                  {data.trail.map((t) => (
                    <OneTrail key={`${t.keepId}:${t.dropId}:${t.mergedAt}`} {...t} />
                  ))}
                </ul>
              )}
            </div>
          </details>
        </>
      ) : null}
    </>
  );
}
