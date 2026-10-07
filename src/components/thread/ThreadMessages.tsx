import { Fragment, useEffect, useMemo, type ReactNode } from "react";
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

export default function ThreadMessages({
  thread,
  tree = false,
}: {
  thread: { rootExtId: string };
  tree?: boolean;
}) {
  const accountId = useAccountId();
  const fetched = $api.useQuery("get", "/v1/chains/{rootExtId}", {
    params: { path: { rootExtId: thread.rootExtId } },
  });

  const entries = useMemo(() => fetched.data?.entries ?? [], [fetched.data?.entries]);
  const { shown, parentOf } = useMemo(() => hoistEdits(entries), [entries]);
  const people = usePersonAddresses();
  const lookup = useMemo(
    () => threadLookup(entries, shown, parentOf, people),
    [entries, shown, parentOf, people],
  );

  const flip = usePreferOriginal();
  const { pulling, pullNote, pull } = useMediaPull({ accountId });
  // Re-attach when entries change: `attach` only wires elements present at the time.
  useEffect(() => {
    const detach = attach(document);
    return detach;
  }, [entries]);
  const { answer, all, setAll, aimed, aim } = useReplyTarget(thread.rootExtId, shown);
  const { landed, endLanding } = useLandOnNewest(thread.rootExtId, shown);

  if (fetched.isError) return <Failure error={fetched.error} />;
  const notice = fetched.isPending
    ? "Loading the thread…"
    : shown.length === 0
      ? "No entries to show."
      : null;
  if (notice) return <p className="mt-2 flex-[1_1_100%] text-xs text-muted">{notice}</p>;

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
        aim={aim}
        flip={flip}
        pulling={pulling}
        pull={pull}
        landed={landed}
        endLanding={endLanding}
      >
        {forest.map((node) => draw(node))}
      </ThreadViewProvider>
      {/* Only when the thread holds a message the mailbox can answer. */}
      {answer ? (
        <ReplyBox
          thread={thread}
          answer={answer}
          answerAnchor={lookup.anchorOf(answer.extId)}
          words={wordsOf(answer)}
          all={all}
          onAll={setAll}
          aimed={aimed}
        />
      ) : null}
    </div>
  );
}
