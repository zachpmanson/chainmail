import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { $api, type OpsMerge, type OpsMergeRecord } from "../lib/api";

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

function when(stamp: string): string {
  const d = new Date(stamp);
  if (Number.isNaN(d.getTime())) return stamp;
  const nowY = new Date().getFullYear();
  const date =
    d.getFullYear() === nowY
      ? d.toLocaleDateString(undefined, { day: "numeric", month: "short" })
      : d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
  const t = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  return `${date}, ${t}`;
}

/** One merge the plan would make. The apply surface is drawn server-side; the
 *  client only offers a checkbox where the plan says applicable, so a request
 *  cannot talk past the boundary. */
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
  const keep = `#${m.keepId} ${m.keepName}` + (m.keepIdentities?.length ? ` · ${m.keepIdentities.join(", ")}` : "");
  const drop = `#${m.dropId} ${m.dropName}` + (m.dropIdentities?.length ? ` · ${m.dropIdentities.join(", ")}` : "");
  return (
    <article className="opmerge">
      <p className="opmrule">
        {m.applicable ? (
          <input
            type="checkbox"
            className="opcheck"
            checked={selected}
            disabled={busy}
            onChange={(e) => onSelect(e.target.checked)}
            aria-label={`Select folding #${m.dropId} ${m.dropName} into #${m.keepId} ${m.keepName}`}
          />
        ) : null}
        {ruleLabel(m.rule)}
        {m.applicable ? <span className="opbad op-apply">apply</span> : <span className="opbad op-ro">read-only</span>}
      </p>
      <p className="opmside">
        keep&nbsp;<code>{keep}</code>
      </p>
      <p className="opmside">
        drop&nbsp;<code>{drop}</code>
      </p>
      {m.evidence ? <p className="opmevidence">{m.evidence}.</p> : null}
    </article>
  );
}

function OneTrail(t: OpsMergeRecord) {
  return (
    <li className="optrail">
      <span className="opwhen">{when(t.mergedAt)}</span>
      <code>#{t.keepId}</code> {t.keepName ?? ""} <span className="oparrow">←</span>{" "}
      <code>#{t.dropId}</code> {t.dropName ?? ""}
      {t.reason ? <span className="opwhy">{t.reason}</span> : null}
    </li>
  );
}

/**
 * The /ops route: the human loop for people merges, served over the same
 * loopback+tunnel boundary as everything else. The dedupe pass computes the
 * same plan the CLI's dry run prints, but here each pair is reviewed against
 * the evidence string and applied individually behind a confirm. Ticking
 * several pairs is one confirm, not one plan: every POST still names a single
 * pair and the server re-derives the plan for each, so a pair an earlier merge
 * in the same batch made moot is refused with the reason rather than assumed to
 * still hold. Tiers the plan shows read-only (first-name-and-org, webmail) have
 * no checkbox; the server refuses them anyway, so the boundary does not depend
 * on this screen's good behaviour.
 */
export function OpsView() {
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
  // Pairs merged since this screen loaded. The refetched plan drops them (the
  // dropped person is gone), and this keeps them from reappearing in the gap
  // while that refetch is in flight.
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

  /**
   * Apply the ticked pairs, one at a time, and stop at the first refusal. Each
   * request re-derives the plan server-side, which is what makes a batch safe:
   * a pair that stopped being applicable mid-batch comes back as a 409 naming
   * why, and the merge after it is never sent.
   */
  async function mergeChosen() {
    const batch = chosen;
    setConfirming(false);
    setError(null);
    setBusy(true);
    let done = 0;
    for (const m of batch) {
      setProgress(`${done + 1} of ${batch.length}`);
      try {
        await apply.mutateAsync({ body: { keepId: m.keepId, dropId: m.dropId } });
      } catch (e) {
        setError(
          `${done} of ${batch.length} merged, then folding #${m.dropId} into #${m.keepId} `+
            `was refused: ${errText(e)}`,
        );
        break;
      }
      done += 1;
      setApplied((prev) => new Set(prev).add(m.dropId));
    }
    setSelected(new Set());
    setBusy(false);
    setProgress(null);
    if (done > 0) setLast(done === 1 ? "merged 1 pair" : `merged ${done} pairs`);
    // Refetch either way: what applied is gone, and a refusal is a statement
    // about the plan, so the screen should show the plan as it is now.
    await qc.invalidateQueries({ queryKey: ["get", "/v1/ops/plan"] });
  }
  return (
    <div className="wrap opswrap">
      <header className="top">
        <h1>ops</h1>
        <p className="sub">
          People merges the dedupe pass would make — reviewed and applied here,
          never by an unattended pass.
        </p>
      </header>

      {plan.isError ? (
        <p className="selfail" role="alert">
          {errText(plan.error)}
        </p>
      ) : null}
      {error ? (
        <p className="selfail" role="alert">
          {error}
        </p>
      ) : null}
      {last ? <p className="opnote">{last} — the plan below is the current one.</p> : null}

      <h2 className="ophead">Merge plan</h2>
      {applicable.length > 0 ? (
        <div className="opactions">
          <label className="opselectall">
            <input
              type="checkbox"
              checked={allPicked}
              disabled={busy}
              ref={(el) => {
                // Some ticked is neither of the two states a checkbox has, and
                // an empty box over a half-selected batch reads as "none".
                if (el) el.indeterminate = selected.size > 0 && !allPicked;
              }}
              onChange={(e) =>
                setSelected(
                  e.target.checked ? new Set(applicable.map((m) => m.dropId)) : new Set(),
                )
              }
            />
            select all {applicable.length} applicable
          </label>
          <button
            type="button"
            className="opbtn opbtn-batch"
            disabled={selected.size === 0 || busy}
            onClick={() => {
              setError(null);
              setConfirming(true);
            }}
          >
            {busy
              ? `merging ${progress ?? ""}`
              : `merge ${selected.size} selected`}
          </button>
        </div>
      ) : null}
      {confirming && chosen.length > 0 ? (
        <div className="opmconfirm">
          <p className="opmwarn">
            <strong>This cannot be undone</strong> — a merge is recorded, never
            reversed. {chosen.length === 1 ? "This pair" : `These ${chosen.length} pairs`} will
            be folded into their keepers now:
          </p>
          <ul className="opmpairs">
            {chosen.map((m) => (
              <li key={m.dropId}>
                <code>#{m.dropId} {m.dropName}</code> <span className="oparrow">→</span>{" "}
                <code>#{m.keepId} {m.keepName}</code>
              </li>
            ))}
          </ul>
          <div className="opmact">
            <button
              type="button"
              className="opbtn opbtn-after"
              disabled={busy}
              onClick={mergeChosen}
            >
              {busy
                ? "merging…"
                : chosen.length === 1
                  ? "merge this pair"
                  : `merge these ${chosen.length} pairs`}
            </button>
            <button
              type="button"
              className="opbtn"
              disabled={busy}
              onClick={() => setConfirming(false)}
            >
              cancel
            </button>
          </div>
        </div>
      ) : null}
      {!data ? (
        <p className="opnote">{plan.isPending ? "Reading the plan…" : "No plan."}</p>
      ) : merges.length === 0 ? (
        <p className="opnote">
          Nothing to merge — every name-only person the pass found has been
          folded, and no other tier is applicable here.
        </p>
      ) : (
        <ol className="oplist">
          {merges.map((m) => (
            <li key={m.dropId} className="oprow">
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
            <summary>{data.refusals.length} refusal{(data.refusals.length === 1 ? "" : "s")} — shown, never applied</summary>
            <div className="pbody">
              {data.refusals.length === 0 ? (
                <p className="opnote">None.</p>
              ) : (
                <ul className="oprefs">
                  {data.refusals.map((r) => (
                    <li key={`${r.rule}:${r.subject}`}>
                      <span className="opwhy">{ruleLabel(r.rule)}</span>{" "}
                      <code>{r.subject}</code> — {r.reason}{" "}
                      <span className="opwhy">(people {r.people.map((p) => `#${p}`).join(", ")})</span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </details>

          <details className="pan">
            <summary>{data.candidates.length} candidate{(data.candidates.length === 1 ? "" : "s")} for a human glance</summary>
            <div className="pbody">
              {data.candidates.length === 0 ? (
                <p className="opnote">None.</p>
              ) : (
                <ul className="oprefs">
                  {data.candidates.map((c) => (
                    <li key={`${c.aId}:${c.bId}`}>
                      <code>{c.aName}</code> ~ <code>{c.bName}</code> — {c.reason}
                      {c.suggest ? <> <code>{c.suggest}</code></> : null}
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
            <div className="pbody">
              {data.twinsDeclined.length === 0 ? (
                <p className="opnote">None — every stored copy collapsed.</p>
              ) : (
                <ul className="oprefs">
                  {data.twinsDeclined.map((d) => (
                    <li key={d.reason}>
                      <span className="opwhy">{d.count}</span> — {d.reason}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </details>

          <details className="pan" open>
            <summary>{data.trail.length === 1 ? "1 merge" : `${data.trail.length} merges`} recorded — the person_merges trail</summary>
            <div className="pbody">
              {data.trail.length === 0 ? (
                <p className="opnote">
                  None yet. The trail is the audit record of every merge, by
                  whatever surface it was made.
                </p>
              ) : (
                <ul className="optrails">
                  {data.trail.map((t) => (
                    <OneTrail key={`${t.keepId}:${t.dropId}:${t.mergedAt}`} {...t} />
                  ))}
                </ul>
              )}
            </div>
          </details>
        </>
      ) : null}
    </div>
  );
}