import Button from "../ui/Button";
import { useEffect, useMemo, useState } from "react";
import Timeline from "./Timeline";
import { attach } from "../../client/behaviour";
import { derive } from "../../lib/timeline/derive";
import SpecView from "./SpecView";
import AddEmailsModal from "./AddEmailsModal";
import ProposalsModal from "./ProposalsModal";
import type { RefreshReport } from "../../lib/api/api";
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
  const [showSpec, setShowSpec] = useState(false);
  const [showAdd, setShowAdd] = useState(false);
  const proposals = useProposalsOpen(report);
  const { chains, excluded, onToggle, filtered } = useThreadFilter(spec);
  const empty = filtered.messages.length === 0;
  useDocumentTitle(spec.title);
  useTranscriptBehaviour(filtered, empty);

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
          className="fixed bottom-[.6rem] left-[.6rem] z-32 px-2 py-1 text-xs text-muted hover:text-accent"
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
          open={proposals.open}
          refreshing={refreshing}
          onClose={proposals.dismiss}
          onAccept={(ids) => {
            onAccept?.(ids);
            proposals.dismiss();
          }}
        />
      ) : null}
    </>
  );
}

// Closing hides proposals until a report without any re-arms them.
function useProposalsOpen(report: RefreshReport | null | undefined) {
  const [dismissed, setDismissed] = useState(false);
  const proposed = Boolean(report?.chainsProposed?.length);
  if (!proposed && dismissed) setDismissed(false);
  return { open: proposed && !dismissed, dismiss: () => setDismissed(true) };
}

function useThreadFilter(spec: Spec) {
  const [excluded, setExcluded] = useState<Set<string>>(new Set());

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

  return { chains, excluded, onToggle, filtered };
}

// Restore the previous title on unmount so the next page doesn't inherit it.
function useDocumentTitle(title: string | undefined) {
  useEffect(() => {
    const previous = document.title;
    const own = (title ?? "").trim();
    document.title = own ? `${own} — Chainmail` : "Chainmail";
    return () => {
      document.title = previous;
    };
  }, [title]);
}

function useTranscriptBehaviour(filtered: Spec, empty: boolean) {
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
}
