import { Fragment, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";
import { ApiError, $api, type CorpusEntry } from "../../lib/api/api";
import { MEDIA_BASE, pullSummary } from "../../lib/message/attachments";
import { attach } from "../../client/behaviour";
import { orgOrder, slotsFor } from "../../lib/timeline/derive";
import { resolveEdits, type EditEntry, type RowEdit } from "../../lib/timeline/edits";
import { newest } from "../../lib/inbox/newest";
import { pushToast } from "../../lib/ui/toasts";
import { gmailIdOf, sourceLine } from "../../lib/message/sources";
import { fetchOriginal } from "../../lib/message/original";
// Aliased: the component's own `tree` prop would shadow it.
import { tree as buildTree, type Knot } from "../../lib/thread/tree";
import { senderTitle, usePersonAddresses, withAddress } from "../../lib/message/who";
import Failure from "./Failure";
import Edits from "../specs/Edits";
import Message from "./Message";
import { type StampData } from "./Message";
import ParticipantsPanel from "./ParticipantsPanel";
import { castOfEntries } from "./Participants";
import ReplyLink from "./ReplyLink";
import { type ReplyTarget } from "./ReplyLink";
import ReplyBox from "../compose/ReplyBox";
import AnswerPress from "../compose/AnswerPress";
import Source from "./Source";

/** With no offset, reads UTC under the stated label, as internal/spec/zones.go does. */
function stampOf(e: CorpusEntry): StampData {
  const at = new Date(e.ts);
  if (Number.isNaN(at.getTime())) return { date: e.ts, zone: "unknown" };
  const label = (e.tz ?? "").trim();
  const offset = e.tzOffsetMinutes;
  const wall = new Date(at.getTime() + (offset ?? 0) * 60_000);
  const stated = label !== "" || offset !== undefined;
  return {
    date: DATE.format(wall),
    time: TIME.format(wall),
    tz: label !== "" ? label : offset !== undefined ? formatOffset(offset) : "",
    zone: stated ? "stated" : "unknown",
  };
}

/** The instant is already shifted into the sender's clock, so format it as UTC. */
const DATE = new Intl.DateTimeFormat("en-GB", {
  weekday: "short",
  day: "numeric",
  month: "short",
  year: "numeric",
  timeZone: "UTC",
});
const TIME = new Intl.DateTimeFormat("en-GB", {
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
  timeZone: "UTC",
});

/** Minutes east of UTC as a Date-header zone, e.g. "+0545". */
function formatOffset(mins: number): string {
  const sign = mins < 0 ? "-" : "+";
  const abs = Math.abs(mins);
  return `${sign}${String(Math.floor(abs / 60)).padStart(2, "0")}${String(abs % 60).padStart(2, "0")}`;
}

/** Ext ids aren't valid HTML anchors, so anchors use the thread position. */
const anchor = (i: number) => `entry-${i}`;

/** Shared by ReplyLink and the reply box so both state the same clock as the bubble. */
function wordsOf(e: CorpusEntry): { who: string; whoTitle: string; when: string } {
  const at = stampOf(e);
  return {
    who: e.author ?? "",
    whoTitle: senderTitle(e),
    when: [at.date, at.time].filter(Boolean).join(" "),
  };
}

export default function ThreadMessages({
  thread,
  tree = false,
}: {
  thread: { rootExtId: string };
  tree?: boolean;
}) {
  const queryClient = useQueryClient();
  const accountId = useSearch({ from: "/" }).accountId;
  const fetched = $api.useQuery("get", "/v1/chains/{rootExtId}", {
    params: { path: { rootExtId: thread.rootExtId } },
  });

  const entries = useMemo(() => fetched.data?.entries ?? [], [fetched.data?.entries]);

  // Hoist edited copies out (as derive.ts does) since their edit draws inside the quoter,
  // and re-point replies to a hoisted copy at the copy's own parent.
  const { shown, parentOf } = useMemo(() => {
    const at = new Map(entries.map((e) => [e.extId, e]));
    const hoisted = new Set<string>();
    for (const e of entries) {
      for (const ed of e.edits ?? []) if (ed.id && at.has(ed.id)) hoisted.add(ed.id);
    }
    const effective = (id?: string): string | undefined => {
      const seen = new Set<string>();
      let cur = id;
      while (cur && at.has(cur) && hoisted.has(cur) && !seen.has(cur)) {
        seen.add(cur);
        cur = at.get(cur)!.parent;
      }
      return cur && at.has(cur) ? cur : undefined;
    };
    const shown = entries.filter((e) => !hoisted.has(e.extId));
    return { shown, parentOf: new Map(shown.map((e) => [e.extId, effective(e.parent)])) };
  }, [entries]);

  // Optimistic: patch every cached chain for this person, then invalidate; roll back on error.
  const prefer = $api.useMutation("post", "/v1/people/{personId}");

  const flipStyle = (personId: number, next: boolean) => {
    queryClient.setQueriesData<{ entries: CorpusEntry[] }>(
      { queryKey: ["get", "/v1/chains/{rootExtId}"] },
      (old) =>
        old && {
          ...old,
          entries: old.entries.map((e) =>
            e.personId === personId ? { ...e, preferOriginal: next } : e,
          ),
        },
    );
    prefer.mutate(
      { params: { path: { personId } }, body: { preferOriginal: next } },
      {
        onError: (err) => {
          pushToast(
            `That reading style did not stick: ${err instanceof Error ? err.message : String(err)}. ` +
              "Nothing was stored — press again to retry.",
            "fail",
          );
        },
        onSettled: () => {
          void queryClient.invalidateQueries({ queryKey: ["get", "/v1/chains/{rootExtId}"] });
        },
      },
    );
  };

  // One pull at a time; the other buttons are held while one is out.
  const [pulling, setPulling] = useState<string | null>(null);
  const [pullNote, setPullNote] = useState<string | null>(null);

  // No spec to patch: re-read the thread, and release the button only once that lands.
  const pull = $api.useMutation("post", "/v1/media/pull", {
    onSuccess: (data) => {
      console.log(`fetch: ${pullSummary(data)}`);
      setPullNote(null);
      void queryClient
        .invalidateQueries({ queryKey: ["get", "/v1/chains/{rootExtId}"] })
        .finally(() => setPulling(null));
    },
    onError: (e) => {
      setPulling(null);
      setPullNote(
        e instanceof ApiError && e.status === 403
          ? "This host cannot fetch files (it was started without -media)."
          : `Fetching the files failed: ${e instanceof Error ? e.message : String(e)}. Nothing was stored — press again to retry.`,
      );
    },
  });

  // Land on the newest entry once per thread; refetches must not move a reader who scrolled.
  const [landed, setLanded] = useState<string | null>(null);
  const landedFor = useRef<string | null>(null);
  // Kept as an ext id because entries are re-read after a send; an id that no longer
  // resolves falls back to the default.
  const [answering, setAnswering] = useState<string | null>(null);
  const [all, setAll] = useState(true);
  // A count, not a flag: re-pressing on the already-answered message must still register.
  const [aimed, setAimed] = useState(0);
  // Above the early returns so the hook count doesn't change once the thread loads.
  const people = usePersonAddresses();
  // Re-attach when entries change: `attach` only wires elements present at the time.
  useEffect(() => {
    const detach = attach(document);
    return detach;
  }, [entries]);
  // Reset the reply target on thread change without remounting, so the draft survives.
  useEffect(() => {
    setAnswering(null);
    setAll(true);
  }, [thread.rootExtId]);
  const target = newest(shown);
  useEffect(() => {
    if (!target || landedFor.current === thread.rootExtId) return;
    landedFor.current = thread.rootExtId;
    const el = document.getElementById(anchor(shown.indexOf(target)));
    // Absent in jsdom.
    el?.scrollIntoView?.({ block: "start" });
    setLanded(target.extId);
  }, [thread.rootExtId, shown, target]);

  if (fetched.isError) return <Failure error={fetched.error} />;
  if (fetched.isPending)
    return <p className="mt-2 flex-[1_1_100%] text-[.78rem] text-muted">Loading the thread…</p>;

  if (shown.length === 0)
    return <p className="mt-2 flex-[1_1_100%] text-[.78rem] text-muted">No entries to show.</p>;

  const slot = slotsFor(orgOrder(shown.map((e) => e.org)));
  // Recipients have no address in the chain read, so the identity graph fills in,
  // and wins over the entry's "address unknown; quoted by" wording.
  const titles = new Map<string, string>();
  for (const e of shown) if (e.author) titles.set(e.author, senderTitle(e));
  for (const e of shown)
    for (const p of e.participants ?? []) {
      const addresses = people.get(p.personId);
      if (addresses) titles.set(p.name, withAddress(p.name, addresses));
    }
  // Built from drawn entries only: a hoisted copy has no row to link to.
  const byExt = new Map<string, CorpusEntry>();
  const indexOf = new Map<string, number>();
  const anchorByGmail = new Map<string, string>();
  shown.forEach((e, i) => {
    byExt.set(e.extId, e);
    indexOf.set(e.extId, i);
    const gmail = gmailIdOf(e);
    if (gmail) anchorByGmail.set(gmail, anchor(i));
  });
  // Includes hoisted copies, which edits diff against.
  const everyExt = new Map(entries.map((e) => [e.extId, e]));
  const mailName = (extId: string): string => {
    const host = byExt.get(extId);
    if (!host) return extId;
    const gmail = gmailIdOf(host);
    return gmail ? `msg ${gmail}` : host.extId;
  };

  // /v1/chains returns the whole thread, so a parent missing here isn't in the corpus.
  const replyOf = (e: CorpusEntry): ReplyTarget | null => {
    const parentId = parentOf.get(e.extId);
    const parent = parentId ? byExt.get(parentId) : undefined;
    if (!parent) return null;
    const i = indexOf.get(parent.extId);
    if (i === undefined) return null;
    const { who, whoTitle, when } = wordsOf(parent);
    return { anchor: anchor(i), who, whoTitle, when };
  };

  // If the base was itself hoisted, the raw id stays and the link points nowhere.
  const editsOf = (e: CorpusEntry, at: StampData): RowEdit[] | undefined => {
    const resolved = resolveEdits(
      e.edits,
      (id) => {
        const copy = id ? everyExt.get(id) : undefined;
        if (!copy) return undefined;
        const cAt = stampOf(copy);
        return {
          html: copy.html ?? "",
          who: copy.author ?? "",
          stamp: [cAt.date, cAt.time].filter(Boolean).join(" "),
        } satisfies EditEntry;
      },
      { who: e.author ?? "", time: at.time ?? "" },
    );
    for (const ed of resolved ?? []) {
      const i = indexOf.get(ed.base);
      if (i !== undefined) ed.base = anchor(i);
    }
    return resolved;
  };

  // Newest, not last drawn (tree view reorders), and only entries the mailbox holds:
  // quote-recovered entries can't be threaded onto, and the server would refuse them.
  const newestAnswer = newest(shown.filter((e) => gmailIdOf(e) !== undefined));

  // Re-check answerability: re-reads can drop an entry's mailbox copy.
  const chosen = shown.find((e) => e.extId === answering && gmailIdOf(e) !== undefined);
  const answer = chosen ?? newestAnswer;

  const aim = (extId: string) => {
    setAnswering(extId);
    setAll(true);
    setAimed((n) => n + 1);
  };

  const forest = tree
    ? buildTree(
        shown,
        (e) => e.extId,
        (e) => parentOf.get(e.extId),
      )
    : shown.map((entry) => ({ entry, replies: [] }));

  const bubble = (e: CorpusEntry) => (
    <Message
      // Position in the corpus order, not draw order, so tree view doesn't rename anchors.
      id={anchor(indexOf.get(e.extId) ?? 0)}
      body={e.html ?? ""}
      sender={e.author}
      senderTitle={senderTitle(e)}
      orgSlot={slot(e.org)}
      me={e.mine}
      quoted={e.quoted}
      to={e.to}
      toTitle={(name) => titles.get(name) ?? name}
      subject={e.subject}
      source={<Source source={sourceLine(e, mailName)} anchorByGmail={anchorByGmail} />}
      reply={<ReplyLink parent={replyOf(e)} />}
      // Only for messages the mailbox holds; the server would refuse the others.
      answer={
        gmailIdOf(e) !== undefined ? (
          <AnswerPress extId={e.extId} pressed={answer?.extId === e.extId} onPress={aim} />
        ) : undefined
      }
      edits={<Edits edits={editsOf(e, stampOf(e))} />}
      stamp={stampOf(e)}
      copyJson={e}
      original={e.original ? { extId: e.extId, load: fetchOriginal } : undefined}
      fromEmail={e.fromEmail}
      // Without a person (quote-recovered entries) the switch falls back to browser storage.
      person={
        e.personId ? { id: e.personId, preferOriginal: e.preferOriginal === true } : undefined
      }
      onPreferOriginal={e.personId ? (next: boolean) => flipStyle(e.personId!, next) : undefined}
      attachments={e.attachments}
      extId={e.extId}
      onPull={(extId) => {
        setPulling(extId);
        setPullNote(null);
        pull.mutate({ body: { entry: extId, ...(accountId ? { accountId } : {}) } });
      }}
      pulling={pulling}
      mediaBase={MEDIA_BASE}
      landed={e.extId === landed}
      onLandedEnd={e.extId === landed ? () => setLanded(null) : undefined}
    />
  );

  /** One container per subtree so its left border runs from the first reply to the last. */
  const draw = (node: Knot<CorpusEntry>): ReactNode => (
    <Fragment key={node.entry.extId}>
      {bubble(node.entry)}
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
          className="mb-3 mt-0 border border-accent border-l-[3px] rounded-md bg-card px-3 py-2 text-[.85rem] text-fg"
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
          orgSlot: slot,
          whoTitle: (name: string) => titles.get(name) ?? name,
        }}
        people={castOfEntries(shown)}
      />
      {forest.map((node) => draw(node))}
      {/* Only when the thread holds a message the mailbox can answer. */}
      {answer ? (
        <ReplyBox
          thread={thread}
          answer={answer}
          answerAnchor={anchor(indexOf.get(answer.extId) ?? 0)}
          words={wordsOf(answer)}
          all={all}
          onAll={setAll}
          aimed={aimed}
        />
      ) : null}
    </div>
  );
}
