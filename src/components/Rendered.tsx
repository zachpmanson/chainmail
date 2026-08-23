import { useEffect, useMemo, useState } from "react";
import { Timeline } from "./Timeline";
import { attach } from "../client/behaviour";
import { derive } from "../lib/derive";
import { SpecView } from "./SpecView";
import type { RefreshCandidate, RefreshReport } from "../lib/api";
import type { Timeline as Spec } from "../lib/spec";

/**
 * A spec with its chain filter and transcript behaviour attached. Excluding a
 * chain re-derives ordering, lanes, spines, the minimap and every count —
 * hiding rows would leave holes in the grid and mis-drawn lanes.
 *
 * Rendered is the shared presentational half of the two page routes and the
 * two legacy ways in (?spec=, drag-drop); whoever owns the spec owns this.
 */
export function Rendered({ spec, onBack, onRefresh, onAccept, report, refreshing, refreshNote }: {
  spec: Spec;
  onBack?: () => void;
  onRefresh?: () => void;
  /** accept a set of proposed chains by root ext id; supplied together with report in the app */
  onAccept?: (ids: string[]) => void;
  /** the last refresh's report, held so its proposals can be evaluated */
  report?: RefreshReport | null;
  refreshing?: boolean;
  refreshNote?: string | null;
}) {
  const [excluded, setExcluded] = useState<Set<string>>(new Set());
  const [showSpec, setShowSpec] = useState(false);
  const [showProposals, setShowProposals] = useState(false);

  // chains of the UNFILTERED trail, so an excluded one stays listed and checkable
  const all = useMemo(() => derive(spec), [spec]);
  const chains = useMemo(
    () =>
      all.layout.chains.map((c) => {
        // link to whichever entry in the chain names a mailbox id; a fully
        // unspooled chain may have none, in which case only the anchor is offered
        const withId = c.entries
          .map((id) => all.rows.find((r) => r.id === id)!)
          .find((r) => r.entry.threadId ?? r.entry.gmailId);
        return {
          root: c.root,
          subject: c.subject,
          opener: c.opener,
          date: c.date,
          count: c.entries.length,
          gmailId: withId?.entry.threadId ?? withId?.entry.gmailId,
          anchor: c.root,
        };
      }),
    [all],
  );

  const filtered = useMemo<Spec>(() => {
    if (excluded.size === 0) return spec;
    const keep = all.rows
      .filter((r) => !excluded.has(r.chain))
      .map((r) => r.entry);
    return { ...spec, messages: keep as Spec["messages"] };
  }, [spec, all, excluded]);

  const onToggle = (root: string) =>
    setExcluded((prev) => {
      const next = new Set(prev);
      if (next.has(root)) next.delete(root);
      else next.add(root);
      return next;
    });

  const proposals = report?.chainsProposed?.length ? report.chainsProposed : [];

  const empty = filtered.messages.length === 0;

  useEffect(() => {
    if (empty) return;
    // StrictMode double-invokes effects in dev; attach() returns a cleanup so the
    // second pass does not stack duplicate listeners
    const detach = attach(document);
    document.body.classList.add("hasmap");
    return detach;
  }, [filtered, empty]);

  if (empty) {
    return (
      <div className="wrap">
        <p style={{ padding: "2rem", color: "var(--muted)" }}>
          Every chain is excluded. Re-enable one from Sources &amp; provenance —
          reload to reset.
        </p>
      </div>
    );
  }

  return (
    <>
      {onBack ? (
        <button type="button" className="selback" onClick={onBack}>
          ← choose chains
        </button>
      ) : null}
      <Timeline
        spec={filtered}
        filter={{ chains, excluded, onToggle }}
        onShowSpec={() => setShowSpec(true)}
        onRefresh={onRefresh}
        onEval={proposals.length ? () => setShowProposals(true) : undefined}
        refreshing={refreshing}
        refreshNote={refreshNote}
      />
      {showSpec ? <SpecView spec={filtered} onClose={() => setShowSpec(false)} /> : null}
      {report?.chainsProposed?.length ? (
        <ProposalsModal
          proposals={report.chainsProposed}
          open={showProposals}
          refreshing={refreshing}
          onClose={() => setShowProposals(false)}
          onAccept={(ids) => { onAccept?.(ids); setShowProposals(false); }}
        />
      ) : null}
    </>
  );
}

/**
 * Modal that shows chains a refresh proposed but did not add. A refresh discovers
 * candidates (the query pass) and curates only what is accepted (the thread pass
 * is applied automatically, so its growth never lands here). Each row names what
 * the server printed for the CLI --accept: the root ext id, the subject and the
 * query that found it, with matched/entries as the honest measure of whether the
 * chain is about the query at all.
 */
function ProposalsModal({ proposals, open, refreshing, onClose, onAccept }: {
  proposals: RefreshCandidate[];
  open: boolean;
  refreshing?: boolean;
  onClose: () => void;
  onAccept: (ids: string[]) => void;
}) {
  const [accepted, setAccepted] = useState<Set<string>>(new Set());
  if (!open) return null;

  const ids = proposals.map((p) => p.rootExtId);
  const toggle = (root: string) =>
    setAccepted((prev) => {
      const next = new Set(prev);
      if (next.has(root)) next.delete(root);
      else next.add(root);
      return next;
    });

  return (
    <div className="proposals" role="dialog" aria-modal="true" aria-label="Proposed chains">
      <div className="proposals-panel">
        <div className="proposals-head">
          <b>proposed</b>
          <span className="note">found by a query, not yet on the page — accept the ones that belong</span>
          <button className="tbtn" type="button" onClick={onClose} disabled={refreshing}>close</button>
        </div>
        <ul className="proposals-list">
          {proposals.map((p) => {
            const on = accepted.has(p.rootExtId);
            return (
              <li key={p.subject ?? p.container ?? p.rootExtId} className="propcard">
                <label className="proptoggle">
                  <input type="checkbox" checked={on} onChange={() => toggle(p.rootExtId)} />
                  <span className="propsubj">{p.subject ?? <em>no subject</em>}</span>
                </label>
                <span className="propmeta">{p.matched}/{p.entries} matched · {p.span ?? ""} · {p.query}</span>
                <code className="proprowid">{p.rootExtId}</code>
              </li>
            );
          })}
        </ul>
        <div className="proposals-foot">
          <button className="tbtn" type="button" disabled={refreshing || accepted.size === 0}
                  onClick={() => onAccept([...accepted])}>
            {refreshing ? "accepting…" : `accept ${accepted.size}`}
          </button>
          <button className="tbtn" type="button" disabled={refreshing}
                  onClick={() => onAccept([...ids])}>
            accept all {proposals.length}
          </button>
        </div>
      </div>
    </div>
  );
}