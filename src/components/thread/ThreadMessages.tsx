import { useDraftCoordinator } from "../compose/DraftSelectionContext";
import { useThreadPopouts, type PopoutLocation } from "../compose/ThreadPopoutContext";
import DraftCards from "../compose/DraftCards";
import InlineAlert from "../ui/InlineAlert";
import { Fragment, useEffect, useMemo, useState, type ReactNode } from "react";
import { $api, type CorpusEntry } from "../../lib/api/api";
import { useAccountId } from "../../lib/inbox/useAccountId";
import { attach } from "../../client/behaviour";
import { usePersonAddresses, wordsOf } from "../../lib/message/who";
import { hoistEdits } from "../../lib/thread/hoist";
import { threadLookup } from "../../lib/thread/lookup";
// Aliased: the component's own `tree` prop would shadow it.
import { tree as buildTree, type Knot } from "../../lib/thread/tree";
import { usePreferOriginal } from "../../lib/thread/usePreferOriginal";
import { useMediaPull } from "../../lib/thread/useMediaPull";
import { useLandOnNewest } from "../../lib/thread/useLandOnNewest";
import { useReplyTarget } from "../../lib/thread/useReplyTarget";
import Failure from "./Failure";
import ParticipantsPanel from "./ParticipantsPanel";
import { castOfEntries } from "./Participants";
import ReplyBox from "../compose/ReplyBox";
import ThreadEntry from "./ThreadEntry";
import ThreadViewProvider from "./ThreadViewProvider";
import useReplyContinuity from "./useReplyContinuity";

export default function ThreadMessages({
  thread,
  tree = false,
}: {
  thread: { rootExtId: string };
  tree?: boolean;
}) {
  const accountId = useAccountId();
  const coordinator = useDraftCoordinator();
  const selected = coordinator?.selection;
  const popouts = useThreadPopouts();
  const moved = popouts?.initialTarget;
  const [dismissedMove, setDismissedMove] = useState<PopoutLocation>();
  const movedTarget =
    moved !== dismissedMove &&
    moved?.root === thread.rootExtId &&
    (!accountId || moved.account === accountId)
      ? moved.target
      : undefined;
  const selectedReply =
    selected?.draft?.kind === "reply" && selected.draft.rootId === thread.rootExtId
      ? selected.draft
      : undefined;
  const fetched = $api.useQuery("get", "/v1/chains/{rootExtId}", {
    params: { path: { rootExtId: thread.rootExtId }, query: { accountId } },
  });

  const entries = useMemo(() => fetched.data?.entries ?? [], [fetched.data?.entries]);
  const { shown, parentOf } = useMemo(() => hoistEdits(entries), [entries]);
  const people = usePersonAddresses();
  const lookup = useMemo(
    () => threadLookup(entries, shown, parentOf, people),
    [entries, shown, parentOf, people],
  );

  const { flip, pending: readingPending } = usePreferOriginal();
  const { pulling, pullNote, pull } = useMediaPull({ accountId });
  // Re-attach when entries change: `attach` only wires elements present at the time.
  useEffect(() => {
    const detach = attach(document);
    return detach;
  }, [entries]);
  const { answer, all, setAll, aimed, aim } = useReplyTarget(
    thread.rootExtId,
    shown,
    selected?.active
      ? {
          entries,
          parent:
            selectedReply?.replyToEntry ??
            (selected.adopting?.destination === thread.rootExtId
              ? selected.adopting.target
              : undefined) ??
            "",
        }
      : movedTarget
        ? { entries, parent: movedTarget }
        : undefined,
    entries,
  );
  const { landed, endLanding } = useLandOnNewest(thread.rootExtId, shown);

  const continuity = useReplyContinuity(answer, thread.rootExtId, accountId, coordinator);
  useEffect(() => {
    if (
      !popouts ||
      !fetched.data ||
      fetched.isPending ||
      fetched.isFetching ||
      fetched.isError ||
      continuity.answer ||
      continuity.scopePending ||
      selected?.active
    )
      return;
    const witness = { root: thread.rootExtId, account: accountId ?? "", mode: "reader" as const };
    popouts.reader.current = witness;
    return () => {
      if (popouts.reader.current === witness) popouts.reader.current = undefined;
    };
  }, [
    popouts,
    accountId,
    fetched.data,
    fetched.isPending,
    fetched.isFetching,
    fetched.isError,
    continuity.answer,
    continuity.scopePending,
    selected?.active,
    thread.rootExtId,
  ]);
  const notice = fetched.isPending
    ? "Loading the thread…"
    : shown.length === 0
      ? "No entries to show."
      : null;

  const forest = tree
    ? buildTree(
        shown,
        (e) => e.extId,
        (e) => parentOf.get(e.extId),
      )
    : shown.map((entry) => ({ entry, replies: [] }));

  /** One container per subtree so its left border runs from the first reply to the last. */
  const draw = (node: Knot<CorpusEntry>): ReactNode => (
    <Fragment key={node.entry.extId}>
      <ThreadEntry entry={node.entry} />
      {node.replies.length ? (
        <div className="replies -mt-2 ml-1 border-l-2 border-line pt-2 pl-4">
          {node.replies.map((n) => draw(n))}
        </div>
      ) : null}
    </Fragment>
  );

  return (
    <div className="stream">
      {fetched.isError ? (
        <Failure error={fetched.error} />
      ) : notice ? (
        <p className="mt-2 flex-[1_1_100%] text-xs text-muted">{notice}</p>
      ) : null}
      {continuity.scopePending ? (
        <InlineAlert>
          Reply target access is not yet confirmed for this account. Your words are retained;
          autosave, preview and send are paused.
        </InlineAlert>
      ) : null}
      {pullNote ? (
        <p
          className="mt-0 mb-3 rounded-md border border-l-3 border-accent bg-card px-3 py-2 text-sm text-fg"
          role="status"
        >
          {pullNote}
        </p>
      ) : null}
      <ParticipantsPanel
        v={{
          rows: shown.map((e) => ({
            entry: { sender: e.author, org: e.org, fromEmail: e.fromEmail },
          })),
          orgSlot: lookup.slot,
          whoTitle: lookup.titleOf,
        }}
        people={castOfEntries(shown)}
      />
      <ThreadViewProvider
        key={thread.rootExtId}
        lookup={lookup}
        answerExtId={answer?.extId}
        aim={(extId) => {
          if (selected?.active) {
            // A different parent must not inherit the saved draft identity.
            if (!coordinator?.approve()) return;
            coordinator.detach(accountId ?? "");
          }
          setDismissedMove(moved);
          aim(extId);
        }}
        flip={flip}
        readingPending={readingPending}
        pulling={pulling}
        pull={pull}
        landed={landed}
        endLanding={endLanding}
      >
        {forest.map((node) => draw(node))}
      </ThreadViewProvider>
      {/* Only when the thread holds a message the mailbox can answer. */}
      {selected?.active && !answer ? (
        selected.loading ? (
          <DraftCards
            accountId={selected.accountId ?? ""}
            drafts={[]}
            loading={false}
            onSelect={(draft) => coordinator?.select(draft)}
          />
        ) : (
          <InlineAlert>
            The selected reply parent is missing or unavailable. No fallback message will be used.
          </InlineAlert>
        )
      ) : null}
      {continuity.answer ? (
        <ReplyBox
          key={continuity.key}
          thread={thread}
          answer={continuity.answer}
          scopePending={continuity.scopePending}
          answerAnchor={continuity.scopePending ? "" : lookup.anchorOf(continuity.answer.extId)}
          words={continuity.scopePending ? { who: "", when: "" } : wordsOf(continuity.answer)}
          all={all}
          onAll={setAll}
          aimed={aimed}
        />
      ) : null}
    </div>
  );
}
