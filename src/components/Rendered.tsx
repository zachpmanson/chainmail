import { Checkbox } from "./Checkbox";
import { DialogShell } from "./DialogShell";
import { FormField } from "./FormField";
import { Button, TextInput } from "./controls";
import { useEffect, useMemo, useState } from "react";
import { Timeline } from "./Timeline";
import { attach } from "../client/behaviour";
import { derive } from "../lib/derive";
import { SpecView } from "./SpecView";
import { ThreadPreview } from "./ThreadPreview";
import { ThreadRow, RankMeta } from "./ThreadRow";
import {
  $api,
  searchQuery,
  type ChainHit,
  type RefreshCandidate,
  type RefreshReport,
} from "../lib/api";
import type { Timeline as Spec } from "../lib/spec";

/**
 * A spec with its thread filter and transcript behaviour attached. Excluding a
 * thread re-derives ordering, lanes, spines, the minimap and every count —
 * hiding rows would leave holes in the grid and mis-drawn lanes.
 *
 * Rendered is the shared presentational half of the two page routes and the
 * two legacy ways in (?spec=, drag-drop); whoever owns the spec owns this.
 */
export function Rendered({
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
        // link to whichever entry in the thread names a mailbox id; a fully
        // unspooled thread may have none, in which case only the anchor is offered
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
      // The reserved column belongs to the transcript that is on screen. Left on
      // <body>, it reserves the minimap's width — `--panel`, fed from the panel's
      // own offsetWidth — on every page visited afterwards for the rest of the
      // session: a picker, the inbox, the status screen, all of them with a
      // couple of hundred pixels of nothing down the right edge and no tree to
      // account for it.
      document.body.classList.remove("hasmap");
      document.body.style.removeProperty("--panel");
    };
  }, [filtered, empty]);

  if (empty) {
    return (
      <div className="wrap">
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
          className="fixed bottom-[.6rem] left-[.6rem] z-[32] px-[.6rem] py-[.3rem] text-[.76rem] text-[var(--muted)] hover:text-[var(--accent)]"
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

/**
 * The "add email" search: find another thread in the corpus by a fresh query
 * and add it to this page. This is the home-page thread selection, scoped to a
 * page instead of a build: the chosen roots go back through the same accept
 * path the refresh's proposals use (POST /v1/refresh accept=), so a thread the
 * recorded queries never find can join the page anyway — being found once is
 * all it takes to name it.
 *
 * The query goes back with them, because this is the one place a search exists
 * that the page does not record. Without it the thread would sit on the page
 * with no provenance: nothing would explain where it came from, and no later
 * refresh could find it again. The server records it before re-deriving, so it
 * is re-run like any recorded search from here on.
 */
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
      <DialogShell label="Add another email" onBackdropClick={onClose}>
        <div className="proposals-head flex items-center gap-[.6rem] border-b border-line p-[.5rem_.8rem]">
          <b className="text-[.72rem] font-bold uppercase tracking-[.09em] text-muted">add email</b>
          <span className="note ml-auto text-[.7rem] text-muted">
            search the corpus for a thread to add to this page
          </span>
        </div>
        <form
          className="addform flex items-center gap-[.5rem] border-b border-line p-[.45rem_.8rem]"
          onSubmit={submit}
        >
          <FormField
            className="flex min-w-0 flex-1 items-center gap-[.4rem] text-[.7rem] text-muted"
            label="Query"
          >
            <TextInput
              autoFocus
              value={q}
              onChange={(e) => setQ(e.target.value)}
              className="min-w-0 flex-1 rounded-[5px] border border-line bg-bg px-[.5rem] py-[.3rem] text-[.78rem] text-fg"
              placeholder="words, a name, an id"
              aria-label="Search query"
            />
          </FormField>
          <Button
            className="!min-h-0 !rounded-[5px] !border-line !bg-bg !px-[.7rem] !py-[.3rem] !text-[.72rem] !font-semibold !text-fg !cursor-pointer hover:!border-accent hover:!text-accent disabled:!cursor-default disabled:!opacity-[.45]"
            type="submit"
            disabled={!q.trim()}
          >
            Search
          </Button>
        </form>
        {results.isError ? (
          <p className="selnote mt-2 flex-[1_1_100%] text-[.78rem] text-muted" role="alert">
            {results.error instanceof Error ? results.error.message : String(results.error)}
          </p>
        ) : null}
        {results.isFetching ? (
          <p className="selnote mt-2 flex-[1_1_100%] text-[.78rem] text-muted">Searching…</p>
        ) : null}
        {asked && !results.isFetching && !results.isError && chains.length === 0 ? (
          <p className="selnote mt-2 flex-[1_1_100%] text-[.78rem] text-muted">
            No thread matched.
          </p>
        ) : null}
        {chains.length > 0 ? (
          <ul className="proposals-list m-0 flex list-none flex-col gap-[.4rem] overflow-auto p-[.5rem_.8rem]">
            {chains.map((c) => (
              <ThreadRow
                key={c.rootExtId}
                thread={c}
                checked={chosen.includes(c.rootExtId)}
                current={false}
                meta={<RankMeta thread={c} />}
                onToggle={() => toggle(c.rootExtId)}
                // Here the row opens the modal, not a pane: this list is already
                // a dialog, and a second column inside one has nowhere to go.
                onOpen={() => setPreview(c)}
              />
            ))}
          </ul>
        ) : null}
        <div className="proposals-foot flex justify-end gap-[.6rem] border-t border-line p-[.5rem_.8rem]">
          <Button
            className="tbtn"
            type="button"
            disabled={chosen.length === 0}
            onClick={() => {
              // asked, not the text box: the box may have been edited since
              // the search ran, and the page records the search that found
              // the thread, not whatever is typed after it.
              if (asked) onAdd([...chosen], asked);
            }}
          >
            {`add ${chosen.length} to page`}
          </Button>
          <Button className="tbtn" type="button" onClick={onClose}>
            close
          </Button>
        </div>
      </DialogShell>
      {preview ? <ThreadPreview thread={preview} onClose={() => setPreview(null)} /> : null}
    </>
  );
}

/**
 * Modal that shows chains a refresh proposed but did not add. A refresh discovers
 * candidates (the query pass) and curates only what is accepted (the thread pass
 * is applied automatically, so its growth never lands here). Each row names what
 * the server printed for the CLI --accept: the root ext id, the subject and the
 * query that found it, with matched/entries as the honest measure of whether the
 * thread is about the query at all.
 */
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
        <div className="proposals-head flex items-center gap-[.6rem] border-b border-line p-[.5rem_.8rem]">
          <b className="text-[.72rem] font-bold uppercase tracking-[.09em] text-muted">proposed</b>
          <span className="note ml-auto text-[.7rem] text-muted">
            found by a query, not yet on the page — accept the ones that belong
          </span>
        </div>
        <ul className="proposals-list m-0 flex list-none flex-col gap-[.4rem] overflow-auto p-[.5rem_.8rem]">
          {proposals.map((p) => {
            const on = accepted.has(p.rootExtId);
            return (
              <li
                key={p.subject ?? p.container ?? p.rootExtId}
                className="propcard flex flex-row items-center gap-[.6rem] rounded-[5px] border border-line bg-bg p-[.5rem_.6rem]"
              >
                <div className="propcard-body flex min-w-0 flex-1 flex-col gap-[.3rem]">
                  <label className="proptoggle flex min-w-0 flex-1 cursor-pointer items-start gap-[.5rem]">
                    <Checkbox
                      className="mt-[.18rem]"
                      accent="org"
                      checked={on}
                      onChange={() => toggle(p.rootExtId)}
                    />
                    <span className="propsubj max-w-full break-words whitespace-normal font-[var(--serif)] font-semibold">
                      {p.subject ?? <em className="not-italic text-muted">no subject</em>}
                    </span>
                  </label>
                  <span className="propmeta mt-[.15rem] text-[.7rem] text-muted">
                    {p.matched}/{p.entries} matched · {p.span ?? ""} · {p.query}
                    {p.semantic
                      ? ` · sim ${p.similarity?.toFixed(2) ?? "–"}${p.lexical ? " (hybrid)" : " (semantic)"}`
                      : p.lexical
                        ? " · word match"
                        : ""}
                  </span>
                  <code className="proprowid mt-[.1rem] max-w-full break-words text-[.66rem] text-accent">
                    {p.rootExtId}
                  </code>
                </div>
                {/* Preview reads the thread as data, the same modal the search
                    page uses, so a proposal can be judged on its entries before
                    it is accepted. Kept out of the toggle label, so ticking it
                    and previewing it never fight over one hit area. */}
                <Button
                  type="button"
                  className="mr-0 shrink-0 whitespace-nowrap px-[.6rem] py-[.3rem] text-[.72rem] font-semibold text-muted hover:border-accent hover:text-accent"
                  aria-haspopup="dialog"
                  onClick={() => setPreview(p)}
                >
                  Preview
                </Button>
              </li>
            );
          })}
        </ul>
        <div className="proposals-foot flex justify-end gap-[.6rem] border-t border-line p-[.5rem_.8rem]">
          <Button
            className="tbtn"
            type="button"
            disabled={refreshing || accepted.size === 0}
            onClick={() => onAccept([...accepted])}
          >
            {refreshing ? "accepting…" : `accept ${accepted.size}`}
          </Button>
          <Button
            className="tbtn"
            type="button"
            disabled={refreshing}
            onClick={() => onAccept([...ids])}
          >
            accept all {proposals.length}
          </Button>
          <Button className="tbtn" type="button" onClick={onClose} disabled={refreshing}>
            close
          </Button>
        </div>
      </DialogShell>
      {preview ? <ThreadPreview thread={preview} onClose={() => setPreview(null)} /> : null}
    </>
  );
}
