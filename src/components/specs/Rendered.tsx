import Checkbox from "../ui/Checkbox";
import DialogShell from "../ui/DialogShell";
import FormField from "../ui/FormField";
import { Button } from "../ui/controls";
import { TextInput } from "../ui/fields";
import { useEffect, useMemo, useState } from "react";
import Timeline from "./Timeline";
import { attach } from "../../client/behaviour";
import { derive } from "../../lib/timeline/derive";
import SpecView from "./SpecView";
import ThreadPreview from "../thread/ThreadPreview";
import FullThreadRow from "../inbox/FullThreadRow";
import RankMeta from "../inbox/RankMeta";
import {
  $api,
  searchQuery,
  type ChainHit,
  type RefreshCandidate,
  type RefreshReport,
} from "../../lib/api/api";
import type { Timeline as Spec } from "../../lib/timeline/spec";

/** Excluding a thread re-derives everything rather than hiding rows, which would
 *  leave holes in the grid and mis-drawn lanes. */
export default function Rendered({
  spec,
  onBack,
  onRefresh,
  onAdd,
  onAccept,
  onPull,
  pulling,
  mediaBase,
  report,
  refreshing,
}: {
  spec: Spec;
  onBack?: () => void;
  onRefresh?: () => void;
  /** add a set of chains found by a fresh search, by root ext id, with the query that found them */
  onAdd?: (ids: string[], query: string) => void;
  /** accept a set of proposed chains by root ext id; supplied together with report in the app */
  onAccept?: (ids: string[]) => void;
  /** fetch one message's attachment bytes; absent unless the host was started with -media */
  onPull?: (extId: string) => void;
  /** the ext id whose files are being fetched, so its button says so */
  pulling?: string | null;
  /** where stored attachment bytes are served; absent in the static export */
  mediaBase?: string;
  /** the last refresh's report, held so its proposals can be evaluated */
  report?: RefreshReport | null;
  refreshing?: boolean;
}) {
  const [excluded, setExcluded] = useState<Set<string>>(new Set());
  const [showSpec, setShowSpec] = useState(false);
  const [showAdd, setShowAdd] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    if (!report?.chainsProposed?.length) setDismissed(false);
  }, [report]);
  const showProposals = Boolean(report?.chainsProposed?.length) && !dismissed;

  // Restore the previous title on unmount so the next page doesn't inherit it.
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
    const keep = all.rows.filter((r) => !excluded.has(r.chain)).map((r) => r.entry);
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
    return () => {
      detach();
      // Otherwise every later page keeps reserving the minimap's width.
      document.body.classList.remove("hasmap");
      document.body.style.removeProperty("--panel");
    };
  }, [filtered, empty]);

  if (empty) {
    return (
      <div className="wrap mx-auto max-w-[76rem] px-5 pt-7 pb-14">
        <p style={{ padding: "2rem", color: "var(--muted)" }}>
          Every thread is excluded. Re-enable one from Sources &amp; provenance — reload to reset.
        </p>
      </div>
    );
  }

  return (
    <>
      {onBack ? (
        <Button
          type="button"
          density="compact"
          className="fixed bottom-[.6rem] left-[.6rem] z-[32] px-2 py-1 text-[.76rem] text-muted hover:text-accent"
          onClick={onBack}
        >
          ← choose chains
        </Button>
      ) : null}
      <Timeline
        spec={filtered}
        filter={{ chains, excluded, onToggle }}
        onShowSpec={() => setShowSpec(true)}
        onRefresh={onRefresh}
        onAdd={onAdd ? () => setShowAdd(true) : undefined}
        onPull={onPull}
        pulling={pulling}
        mediaBase={mediaBase}
        refreshing={refreshing}
      />
      {showSpec ? <SpecView spec={filtered} onClose={() => setShowSpec(false)} /> : null}
      {showAdd && onAdd ? (
        <AddEmailsModal
          onClose={() => setShowAdd(false)}
          onAdd={(ids, q) => {
            onAdd(ids, q);
            setShowAdd(false);
          }}
        />
      ) : null}
      {report?.chainsProposed?.length ? (
        <ProposalsModal
          proposals={report.chainsProposed}
          open={showProposals}
          refreshing={refreshing}
          onClose={() => setDismissed(true)}
          onAccept={(ids) => {
            onAccept?.(ids);
            setDismissed(true);
          }}
        />
      ) : null}
    </>
  );
}

/** Sends the query with the chosen roots so the page records it and refreshes can
 *  re-find the thread. */
function AddEmailsModal({
  onClose,
  onAdd,
}: {
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
    setChosen([]);
    setAsked(t);
  };

  const toggle = (root: string) =>
    setChosen((prev) => (prev.includes(root) ? prev.filter((r) => r !== root) : [...prev, root]));

  const chains = results.data?.chains ?? [];
  return (
    <>
      <DialogShell label="Add another email" onBackdropClick={onClose}>
        <div className="flex items-center gap-2 border-b border-line py-2 px-3">
          <b className="text-[.72rem] font-bold uppercase tracking-[.09em] text-muted">add email</b>
          <span className="ml-auto text-[.7rem] text-muted">
            search the corpus for a thread to add to this page
          </span>
        </div>
        <form className="flex items-center gap-2 border-b border-line py-2 px-3" onSubmit={submit}>
          <FormField
            className="flex min-w-0 flex-1 items-center gap-1.5 text-[.7rem] text-muted"
            label="Query"
          >
            <TextInput
              autoFocus
              value={q}
              onChange={(e) => setQ(e.target.value)}
              className="min-w-0 flex-1 rounded-[5px] border border-line bg-bg px-2 py-1 text-[.78rem] text-fg"
              placeholder="words, a name, an id"
              aria-label="Search query"
            />
          </FormField>
          <Button
            className="!min-h-0 !rounded-[5px] !border-line !bg-bg !px-3 !py-1 !text-[.72rem] !font-semibold !text-fg !cursor-pointer hover:!border-accent hover:!text-accent disabled:!cursor-default disabled:!opacity-[.45]"
            type="submit"
            disabled={!q.trim()}
          >
            Search
          </Button>
        </form>
        {results.isError ? (
          <p className="mt-2 flex-[1_1_100%] text-[.78rem] text-muted" role="alert">
            {results.error instanceof Error ? results.error.message : String(results.error)}
          </p>
        ) : null}
        {results.isFetching ? (
          <p className="mt-2 flex-[1_1_100%] text-[.78rem] text-muted">Searching…</p>
        ) : null}
        {asked && !results.isFetching && !results.isError && chains.length === 0 ? (
          <p className="mt-2 flex-[1_1_100%] text-[.78rem] text-muted">No thread matched.</p>
        ) : null}
        {chains.length > 0 ? (
          <ul className="m-0 flex list-none flex-col gap-1.5 overflow-auto py-2 px-3">
            {chains.map((c) => (
              <FullThreadRow
                key={c.rootExtId}
                thread={c}
                checked={chosen.includes(c.rootExtId)}
                current={false}
                meta={<RankMeta thread={c} />}
                onToggle={() => toggle(c.rootExtId)}
                onOpen={() => setPreview(c)}
              />
            ))}
          </ul>
        ) : null}
        <div className="flex justify-end gap-2 border-t border-line py-2 px-3">
          <Button
            type="button"
            disabled={chosen.length === 0}
            onClick={() => {
              // The search that ran, not whatever the box holds now.
              if (asked) onAdd([...chosen], asked);
            }}
          >
            {`add ${chosen.length} to page`}
          </Button>
          <Button type="button" onClick={onClose}>
            close
          </Button>
        </div>
      </DialogShell>
      {preview ? <ThreadPreview thread={preview} onClose={() => setPreview(null)} /> : null}
    </>
  );
}

function ProposalsModal({
  proposals,
  open,
  refreshing,
  onClose,
  onAccept,
}: {
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
      <DialogShell label="Proposed chains">
        <div className="flex items-center gap-2 border-b border-line py-2 px-3">
          <b className="text-[.72rem] font-bold uppercase tracking-[.09em] text-muted">proposed</b>
          <span className="ml-auto text-[.7rem] text-muted">
            found by a query, not yet on the page — accept the ones that belong
          </span>
        </div>
        <ul className="m-0 flex list-none flex-col gap-1.5 overflow-auto py-2 px-3">
          {proposals.map((p) => {
            const on = accepted.has(p.rootExtId);
            return (
              <li
                key={p.subject ?? p.container ?? p.rootExtId}
                className="flex flex-row items-center gap-2 rounded-[5px] border border-line bg-bg p-2"
              >
                <div className="flex min-w-0 flex-1 flex-col gap-1">
                  <label className="flex min-w-0 flex-1 cursor-pointer items-start gap-2">
                    <Checkbox
                      className="mt-1"
                      accent="org"
                      checked={on}
                      onChange={() => toggle(p.rootExtId)}
                    />
                    <span className="max-w-full break-words whitespace-normal font-[var(--serif)] font-semibold">
                      {p.subject ?? <em className="not-italic text-muted">no subject</em>}
                    </span>
                  </label>
                  <span className="mt-0.5 text-[.7rem] text-muted">
                    {p.matched}/{p.entries} matched · {p.span ?? ""} · {p.query}
                    {p.semantic
                      ? ` · sim ${p.similarity?.toFixed(2) ?? "–"}${p.lexical ? " (hybrid)" : " (semantic)"}`
                      : p.lexical
                        ? " · word match"
                        : ""}
                  </span>
                  <code className="mt-0.5 max-w-full break-words text-[.66rem] text-accent">
                    {p.rootExtId}
                  </code>
                </div>
                <Button
                  type="button"
                  className="mr-0 shrink-0 whitespace-nowrap px-2 py-1 text-[.72rem] font-semibold text-muted hover:border-accent hover:text-accent"
                  aria-haspopup="dialog"
                  onClick={() => setPreview(p)}
                >
                  Preview
                </Button>
              </li>
            );
          })}
        </ul>
        <div className="flex justify-end gap-2 border-t border-line py-2 px-3">
          <Button
            type="button"
            disabled={refreshing || accepted.size === 0}
            onClick={() => onAccept([...accepted])}
          >
            {refreshing ? "accepting…" : `accept ${accepted.size}`}
          </Button>
          <Button type="button" disabled={refreshing} onClick={() => onAccept([...ids])}>
            accept all {proposals.length}
          </Button>
          <Button type="button" onClick={onClose} disabled={refreshing}>
            close
          </Button>
        </div>
      </DialogShell>
      {preview ? <ThreadPreview thread={preview} onClose={() => setPreview(null)} /> : null}
    </>
  );
}
