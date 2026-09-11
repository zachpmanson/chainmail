import { useEffect, useMemo, useState } from "react";
import { Timeline } from "./Timeline";
import { attach } from "../client/behaviour";
import { derive } from "../lib/derive";
import { SpecView } from "./SpecView";
import { ChainPreview } from "./ChainPreview";
import { ChainRow } from "./Select";
import { $api, searchQuery, type ChainHit, type RefreshCandidate, type RefreshReport } from "../lib/api";
import type { Timeline as Spec } from "../lib/spec";

/**
 * A spec with its chain filter and transcript behaviour attached. Excluding a
 * chain re-derives ordering, lanes, spines, the minimap and every count —
 * hiding rows would leave holes in the grid and mis-drawn lanes.
 *
 * Rendered is the shared presentational half of the two page routes and the
 * two legacy ways in (?spec=, drag-drop); whoever owns the spec owns this.
 */
export function Rendered({ spec, onBack, onRefresh, onAdd, onAccept, report, refreshing, refreshNote, slurpNote }: {
  spec: Spec;
  onBack?: () => void;
  onRefresh?: () => void;
  /** add a set of chains found by a fresh search, by root ext id, with the query that found them */
  onAdd?: (ids: string[], query: string) => void;
  /** accept a set of proposed chains by root ext id; supplied together with report in the app */
  onAccept?: (ids: string[]) => void;
  /** the last refresh's report, held so its proposals can be evaluated */
  report?: RefreshReport | null;
  refreshing?: boolean;
  refreshNote?: string | null;
  /** the transcript of the last slurp, when the server could run one */
  slurpNote?: string | null;
}) {
  const [excluded, setExcluded] = useState<Set<string>>(new Set());
  const [showSpec, setShowSpec] = useState(false);
  const [showAdd, setShowAdd] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  // Auto-open the proposal evaluator whenever a fresh report brings proposals,
  // so the reader is not sent hunting for a separate eval button. Closers keep
  // it hidden for the current report; the next refresh report with proposals
  // reopens it.
  useEffect(() => {
    if (!report?.chainsProposed?.length) setDismissed(false);
  }, [report]);
  const showProposals = Boolean(report?.chainsProposed?.length) && !dismissed;

  // Every spec names its own tab title: the shell serves one static <title>
  // for every route, and the spec is the only thing that knows what it holds.
  // Restoring the previous title on unload keeps the next page from inheriting
  // this one's name.
  useEffect(() => {
    const previous = document.title;
    const own = (spec.title ?? "").trim();
    document.title = own ? `${own} — Chainmail` : "Chainmail";
    return () => {
      document.title = previous;
    };
  }, [spec.title]);

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
        onAdd={onAdd ? () => setShowAdd(true) : undefined}
        refreshing={refreshing}
        refreshNote={refreshNote}
        slurpNote={slurpNote}
      />
      {showSpec ? <SpecView spec={filtered} onClose={() => setShowSpec(false)} /> : null}
      {showAdd && onAdd ? (
        <AddEmailsModal onClose={() => setShowAdd(false)}
                        onAdd={(ids, q) => { onAdd(ids, q); setShowAdd(false); }} />
      ) : null}
      {report?.chainsProposed?.length ? (
        <ProposalsModal
          proposals={report.chainsProposed}
          open={showProposals}
          refreshing={refreshing}
          onClose={() => setDismissed(true)}
          onAccept={(ids) => { onAccept?.(ids); setDismissed(true); }}
        />
      ) : null}
    </>
  );
}

/**
 * The "add email" search: find another chain in the corpus by a fresh query
 * and add it to this page. This is the home-page chain selection, scoped to a
 * page instead of a build: the chosen roots go back through the same accept
 * path the refresh's proposals use (POST /v1/refresh accept=), so a chain the
 * recorded queries never find can join the page anyway — being found once is
 * all it takes to name it.
 *
 * The query goes back with them, because this is the one place a search exists
 * that the page does not record. Without it the chain would sit on the page
 * with no provenance: nothing would explain where it came from, and no later
 * refresh could find it again. The server records it before re-deriving, so it
 * is re-run like any recorded search from here on.
 */
function AddEmailsModal({ onClose, onAdd }: {
  onClose: () => void;
  onAdd: (ids: string[], query: string) => void;
}) {
  const [q, setQ] = useState("");
  const [asked, setAsked] = useState<string | null>(null);
  const [chosen, setChosen] = useState<string[]>([]);
  const [preview, setPreview] = useState<ChainHit | null>(null);

  const results = $api.useQuery(
    "get",
    "/v1/search",
    { params: { query: asked ? searchQuery({ q: asked, mode: "hybrid" }) : {} } },
    { enabled: asked !== null },
  );

  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  const submit = (ev: React.FormEvent) => {
    ev.preventDefault();
    const t = q.trim();
    if (!t) return;
    // A new search invalidates the selection, same rule as the home page: the
    // ids were chosen out of the old candidate list.
    setChosen([]);
    setAsked(t);
  };

  const toggle = (root: string) =>
    setChosen((prev) => (prev.includes(root) ? prev.filter((r) => r !== root) : [...prev, root]));

  const chains = results.data?.chains ?? [];
  return (
    <>
      <div className="proposals" role="dialog" aria-modal="true" aria-label="Add another email" onClick={onClose}>
      <div className="proposals-panel" onClick={(e) => e.stopPropagation()}>
        <div className="proposals-head">
          <b>add email</b>
          <span className="note">search the corpus for a chain to add to this page</span>
        </div>
        <form className="addform" onSubmit={submit}>
          <label>
            <span>Query</span>
            <input
              autoFocus
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="words, a name, an id"
              aria-label="Search query"
            />
          </label>
          <button type="submit" disabled={!q.trim()}>
            Search
          </button>
        </form>
        {results.isError ? (
          <p className="selnote" role="alert">
            {results.error instanceof Error ? results.error.message : String(results.error)}
          </p>
        ) : null}
        {results.isFetching ? <p className="selnote">Searching…</p> : null}
        {asked && !results.isFetching && !results.isError && chains.length === 0 ? (
          <p className="selnote">No chain matched.</p>
        ) : null}
        {chains.length > 0 ? (
          <ul className="proposals-list">
            {chains.map((c) => (
              <ChainRow
                key={c.rootExtId}
                chain={c}
                checked={chosen.includes(c.rootExtId)}
                onToggle={() => toggle(c.rootExtId)}
                onPreview={() => setPreview(c)}
              />
            ))}
          </ul>
        ) : null}
        <div className="proposals-foot">
          <button className="tbtn" type="button" disabled={chosen.length === 0}
                  onClick={() => {
                    // asked, not the text box: the box may have been edited since
                    // the search ran, and the page records the search that found
                    // the chain, not whatever is typed after it.
                    if (asked) onAdd([...chosen], asked);
                  }}>
            {`add ${chosen.length} to page`}
          </button>
          <button className="tbtn" type="button" onClick={onClose}>
            close
          </button>
        </div>
        </div>
      </div>
      {preview ? <ChainPreview chain={preview} onClose={() => setPreview(null)} /> : null}
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
  // The proposal being previewed, by root ext id. Null when no modal is open.
  const [preview, setPreview] = useState<RefreshCandidate | null>(null);
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
    <>
      <div className="proposals" role="dialog" aria-modal="true" aria-label="Proposed chains">
        <div className="proposals-panel">
        <div className="proposals-head">
          <b>proposed</b>
          <span className="note">found by a query, not yet on the page — accept the ones that belong</span>
        </div>
        <ul className="proposals-list">
          {proposals.map((p) => {
            const on = accepted.has(p.rootExtId);
            return (
              <li key={p.subject ?? p.container ?? p.rootExtId} className="propcard">
                <div className="propcard-body">
                  <label className="proptoggle">
                    <input type="checkbox" checked={on} onChange={() => toggle(p.rootExtId)} />
                    <span className="propsubj">{p.subject ?? <em>no subject</em>}</span>
                  </label>
                  <span className="propmeta">
                    {p.matched}/{p.entries} matched · {p.span ?? ""} · {p.query}
                    {p.semantic ? ` · sim ${p.similarity?.toFixed(2) ?? "–"}${p.lexical ? " (hybrid)" : " (semantic)"}` : p.lexical ? " · word match" : ""}
                  </span>
                  <code className="proprowid">{p.rootExtId}</code>
                </div>
                {/* Preview reads the chain as data, the same modal the search
                    page uses, so a proposal can be judged on its entries before
                    it is accepted. Kept out of the toggle label, so ticking it
                    and previewing it never fight over one hit area. */}
                <button type="button" className="selpvbtn" aria-haspopup="dialog"
                        onClick={() => setPreview(p)}>
                  Preview
                </button>
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
          <button className="tbtn" type="button" onClick={onClose} disabled={refreshing}>close</button>
          </div>
        </div>
      </div>
      {preview ? <ChainPreview chain={preview} onClose={() => setPreview(null)} /> : null}
    </>
  );
}