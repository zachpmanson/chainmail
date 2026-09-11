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
 *  client only renders a button where the plan says applicable, so a request
 *  cannot talk past the boundary. */
function MergeCard({
  m,
  confirming,
  busy,
  onConfirm,
  onCancel,
  onApply,
}: {
  m: OpsMerge;
  confirming: boolean;
  busy: boolean;
  onConfirm: () => void;
  onCancel: () => void;
  onApply: () => void;
}) {
  const keep = `#${m.keepId} ${m.keepName}` + (m.keepIdentities?.length ? ` · ${m.keepIdentities.join(", ")}` : "");
  const drop = `#${m.dropId} ${m.dropName}` + (m.dropIdentities?.length ? ` · ${m.dropIdentities.join(", ")}` : "");
  return (
    <article className="opmerge">
      <p className="opmrule">
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
      {m.applicable &&
        (confirming ? (
          <p className="opmconfirm">
            Fold the dropped person into the keeper.{" "}
            <strong>This cannot be undone</strong> — a merge is recorded, never reversed.
            <button type="button" className="opbtn opbtn-after" disabled={busy} onClick={onApply}>
              {busy ? "Merging…" : `Merge #${m.dropId} into #${m.keepId}`}
            </button>
            <button type="button" className="opbtn" disabled={busy} onClick={onCancel}>
              Cancel
            </button>
          </p>
        ) : (
          <button type="button" className="opbtn" onClick={onConfirm}>
            Merge
          </button>
        ))}
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
 * the evidence string and applied individually behind a confirm — there is
 * deliberately no apply-all. Tiers the plan shows read-only (first-name-and-org,
 * webmail) have no button; the server refuses them anyway, so the boundary does
 * not depend on this screen's good behaviour.
 */
export function OpsView() {
  const qc = useQueryClient();
  const plan = $api.useQuery("get", "/v1/ops/plan", {});
  // The dropId whose pair is past the first click, waiting on the confirm.
  const [confirming, setConfirming] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [last, setLast] = useState<string | null>(null);
  const apply = $api.useMutation("post", "/v1/ops/merge", {
    onSuccess: (data) => {
      setLast(`merged #${data.merge.dropId} into #${data.merge.keepId}`);
      setConfirming(null);
      // The plan changed: refetch it so the screen shows what is left, not what
      // it applied.
      qc.invalidateQueries({ queryKey: ["get", "/v1/ops/plan"] });
    },
    onError: (e) => setError(errText(e)),
  });

  const data = plan.data;
  // Applicable pairs first: they are what this screen exists to act on.
  const merges = data
    ? [...data.merges].sort((a, b) => Number(b.applicable) - Number(a.applicable))
    : [];
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
                confirming={confirming === m.dropId}
                busy={apply.isPending}
                onConfirm={() => {
                  setError(null);
                  setConfirming(m.dropId);
                }}
                onCancel={() => setConfirming(null)}
                onApply={() =>
                  apply.mutate({ body: { keepId: m.keepId, dropId: m.dropId } })
                }
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