import type { OpsMergeRecord, OpsPlanResponse } from "../../lib/api/api";
import { ruleLabel } from "../../lib/ops/opsMerges";
import { when } from "../../lib/ui/stamp";

function OneTrail(t: OpsMergeRecord) {
  return (
    <li className="py-1 text-sm leading-normal">
      <span className="text-xs tabular-nums text-muted">{when(t.mergedAt)}</span>
      <code>#{t.keepId}</code> {t.keepName ?? ""} <span className="text-muted">←</span>{" "}
      <code>#{t.dropId}</code> {t.dropName ?? ""}
      {t.reason ? <span className="block text-xs text-muted">{t.reason}</span> : null}
    </li>
  );
}

export default function MergePlanDetails({ data }: { data: OpsPlanResponse }) {
  return (
    <>
      <details className="pan" open={data.refusals.length > 0}>
        <summary>
          {data.refusals.length} refusal{data.refusals.length === 1 ? "" : "s"} — shown, never
          applied
        </summary>
        <div>
          {data.refusals.length === 0 ? (
            <p className="my-1.5 mb-2 text-xs text-muted">None.</p>
          ) : (
            <ul className="mt-1 mb-0 list-none p-0 [&_li]:py-1 [&_li]:text-xs [&_li]:leading-normal [&_li_code]:text-xs">
              {data.refusals.map((r) => (
                <li key={`${r.rule}:${r.subject}`}>
                  <span className="text-muted">{ruleLabel(r.rule)}</span> <code>{r.subject}</code> —{" "}
                  {r.reason}{" "}
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
          {data.candidates.length} candidate{data.candidates.length === 1 ? "" : "s"} for a human
          glance
        </summary>
        <div>
          {data.candidates.length === 0 ? (
            <p className="my-1.5 mb-2 text-xs text-muted">None.</p>
          ) : (
            <ul className="mt-1 mb-0 list-none p-0 [&_li]:py-1 [&_li]:text-xs [&_li]:leading-normal [&_li_code]:text-xs">
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
            <p className="my-1.5 mb-2 text-xs text-muted">None — every stored copy collapsed.</p>
          ) : (
            <ul className="mt-1 mb-0 list-none p-0 [&_li]:py-1 [&_li]:text-xs [&_li]:leading-normal [&_li_code]:text-xs">
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
            <p className="my-1.5 mb-2 text-xs text-muted">
              None yet. The trail is the audit record of every merge, by whatever surface it was
              made.
            </p>
          ) : (
            <ul className="mt-1 mb-0 list-none p-0 [&_li]:py-1 [&_li]:text-sm [&_li]:leading-normal">
              {data.trail.map((t) => (
                <OneTrail key={`${t.keepId}:${t.dropId}:${t.mergedAt}`} {...t} />
              ))}
            </ul>
          )}
        </div>
      </details>
    </>
  );
}
