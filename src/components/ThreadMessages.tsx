import { Fragment, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError, $api, type CorpusEntry } from "../lib/api";
import { MEDIA_BASE, pullSummary } from "../lib/attachments";
import { attach } from "../client/behaviour";
import { orgOrder, slotsFor } from "../lib/derive";
import { resolveEdits, type EditEntry, type RowEdit } from "../lib/edits";
import { newest } from "../lib/newest";
import { pushToast } from "../lib/toasts";
import { gmailIdOf, sourceLine } from "../lib/sources";
import { fetchOriginal } from "../lib/original";
// `buildTree` rather than `tree` because the pane's own prop for the same idea is
// called `tree`, and a prop of that name would shadow the builder here.
import { tree as buildTree, type Knot } from "../lib/tree";
// senderTitle moved to lib/who when the hover titles stopped being only the
// bubbles' business: the pane, the participants panel, the reply box and the
// preview all say where a name's address came from, and one module holds the
// two ways of saying it.
import { senderTitle, usePersonAddresses, withAddress } from "../lib/who";
import { Failure } from "./ThreadPreview";
import { Edits } from "./Edits";
import { Message, type StampData } from "./Message";
import { ParticipantsPanel, castOfEntries } from "./Participants";
import { ReplyLink, type ReplyTarget } from "./ReplyLink";
import { ReplyBox } from "./ReplyBox";
import { Source } from "./Source";

/**
 * A thread in the reading pane, drawn with the transcript's own `Message`.
 *
 * The pane used to read `/v1/chains` and draw its own cards: plain text, a
 * sender, a day. The page built from the same thread drew the spec pipeline's
 * bubbles — the sender's own formatting, the clock with the zone it was stated
 * in, quote markers, attachments — so the same conversation looked like two
 * different products depending on which of them you were looking at. The
 * component is now shared, and the corpus renders the body (`html`) with the
 * same conversion a build uses, so a bubble here is a bubble there.
 *
 * The files a message carries come with the thread, mapped through the same
 * spec.AttachmentOf a build uses, so the chip in the pane is the chip on the page
 * — name, kind, size wording and all — rather than a second reading of the same
 * rows. The bytes are reached from here where the server serves them
 * (/v1/attachments/{sha}, the -media grant), and the sender's own link where it
 * does not: a chip that cannot be opened is still worth showing, because "there
 * was a file on this" is part of what the message said.
 *
 * The two halves of that are the two halves a built page has, and the pane offers
 * both: the chip opens bytes the corpus holds, and `fetch files` under a message
 * whose files it does not hold asks for them (POST /v1/media/pull, the same grant
 * the page's own button presses). The difference is only what a pane does with the
 * answer — a page takes back a rebuilt spec, while the pane draws the corpus and
 * so re-reads the thread it is already holding (see the pull below).
 *
 * The organisation behind the colour does come with the thread, resolved by the
 * same resolver a page build uses, so a bubble here is coloured like the bubble
 * the page draws for the same sender. The slots are then assigned here, in the
 * order the thread's own entries present them — see orgOrder — because a pane has
 * only the entries it was handed and no panel to take organisations from.
 *
 * The line that says what a message answers is the page's own mark too, from one
 * component (see ReplyLink). What differs between the two renderers is only where
 * a parent is found — the spec's rows there, the entries handed to the pane here
 * — and the pane resolves it against the thread it is already holding, so the link
 * lands on the parent's bubble on this page rather than asking anyone for it.
 *
 * A quoter's edit to a quoted message is the page's own mark as well, from the
 * page's own component (see Edits): `/v1/chains` carries the relation,
 * `spec.RenderTrail` resolves it with the same function a build does, and the
 * pane draws it inside the message that made it. The derived copy it names is
 * hoisted out of the transcript rather than drawn as a bubble of its own — that
 * ghost is what issue #42 was about.
 */

/** The transcript's clock, written the way the spec writes it ("Mon 2 Jan 2006",
 *  "15:04"), from the instant and the offset the corpus stored.
 *
 *  With no offset the clock is read in UTC under whatever label the source
 *  stated — the same fallback internal/spec/zones.go makes, and for the same
 *  reason: a label the table cannot turn into an offset is the sender's own word
 *  about their clock, and the pair (label, UTC clock) is at least auditable. The
 *  one thing this cannot do is the page's inference, which reads every placement
 *  in the corpus; here a zone the source did not state is simply unknown. */
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

/** Fixed locale and timeZone: the instant has already been shifted into the
 *  sender's clock above, so reading it as UTC is what prints that clock. A
 *  locale-dependent formatter would print a different date in a different
 *  browser, which is exactly the kind of difference a transcript may not have. */
const DATE = new Intl.DateTimeFormat("en-GB", {
  weekday: "short", day: "numeric", month: "short", year: "numeric", timeZone: "UTC",
});
const TIME = new Intl.DateTimeFormat("en-GB", {
  hour: "2-digit", minute: "2-digit", hour12: false, timeZone: "UTC",
});

/** Minutes east of UTC as a Date-header zone, e.g. "+0545". */
function formatOffset(mins: number): string {
  const sign = mins < 0 ? "-" : "+";
  const abs = Math.abs(mins);
  return `${sign}${String(Math.floor(abs / 60)).padStart(2, "0")}${String(abs % 60).padStart(2, "0")}`;
}

/** The anchor a bubble gets. The entry's own ext id is the only stable handle
 *  here, and it is not an id an HTML anchor can carry, so the thread's position
 *  names it: the timestamp's self-link only has to land on the message it is
 *  part of. */
const anchor = (i: number) => `entry-${i}`;


/** The sender's name and the clock of a message, in the words its own bubble
 *  prints — "Lena Whitfield" and "Mon 2 Mar 2026 09:15".
 *
 *  One function, two callers with the same obligation: the line that says what a
 *  message answers (ReplyLink) and the reply box that says which message is being
 *  answered. Both are claims about a message the reader is looking at, and the
 *  bubble between them states the same clock from the same source — so a reader
 *  must not be told two different times for one message by two lines of the same
 *  pane. */
function wordsOf(e: CorpusEntry): { who: string; whoTitle: string; when: string } {
  const at = stampOf(e);
  return {
    who: e.author ?? "",
    // The same string the bubble wears, because it is the same claim: this line
    // names the person the message is from, and the address is the part of that a
    // reader can check.
    whoTitle: senderTitle(e),
    when: [at.date, at.time].filter(Boolean).join(" "),
  };
}

export function ThreadMessages({
  thread,
  tree = false,
}: {
  thread: { rootExtId: string };
  /** Draw the replies as a tree under the message they answer, rather than in the
   *  order they were sent (see lib/tree). The pane's own switch is what sets it,
   *  and the default is the transcript's own order — a caller that knows nothing
   *  about the tree gets what this component has always drawn. */
  tree?: boolean;
}) {
  const queryClient = useQueryClient();
  const fetched = $api.useQuery("get", "/v1/chains/{rootExtId}", {
    params: { path: { rootExtId: thread.rootExtId } },
  });

  const entries = fetched.data?.entries ?? [];

  // A quoter's edit to a quoted message is drawn INSIDE the message that quoted
  // it, so the derived copy that relation names is not a bubble of its own: it is
  // hoisted out of the transcript here, exactly as a page build hoists it out of
  // its rows (see derive.ts). Without this the pane drew that copy as a second
  // bubble under the message it was edited from — the floating duplicate issue
  // #42 was about — and drew no edit at all, because the copy's id is what the
  // page's own mark is anchored through.
  //
  // A reply anchored to a hoisted copy is re-pointed at the copy's own parent (the
  // base it derives from), so the link under it lands on a bubble that is on this
  // page rather than on an id nothing occupies. The reply graph is the corpus's,
  // walked server side; this is where that meets the renderer's own rows.
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

  // How a sender's mail is read is the one thing this pane writes about a person
  // rather than about a message, and it is written to the corpus rather than kept
  // in this browser: the reader who has decided how to read a run of
  // notifications has decided it on the phone as well as here, and a merge of two
  // spellings of the same person cannot lose it (see the corpus's
  // people.prefer_original). The control is drawn from the entry the chain read
  // already carried, so a bubble costs no second request to know what to show.
  //
  // The write is optimistic, because the switch it answers is a switch: pressing
  // it and waiting a round trip to see it move reads as the press not landing.
  // The cached chains are patched first — every bubble of that sender in every
  // thread this session has read, since the answer is per person and not per
  // message — and then marked stale, so the corpus's own answer is what is drawn
  // as soon as it arrives. A failure takes the patch back the same way: what
  // stands is what the corpus says, not what this pane hoped.
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

  // The message whose files are being fetched. One at a time: a pull is mailbox
  // round trips, and a pane is a place a reader reads rather than a queue they
  // fill — so every other button is held while one is out, exactly as the page's
  // own button holds the rest (see Attachments).
  const [pulling, setPulling] = useState<string | null>(null);
  // Why the last pull failed, when it did. It goes on the pane and not only in
  // the console, for the reason it does on the page: a press that spends mailbox
  // round trips and then fails must not look like nothing happening.
  const [pullNote, setPullNote] = useState<string | null>(null);

  // Fetch one message's attachment bytes into the corpus.
  //
  // The pane draws the corpus, so there is nothing to patch when the bytes land:
  // the files appear by re-reading the thread that is already on screen. That is
  // the one way this differs from the page, which is handed the rebuilt spec by
  // the endpoint and has a name to be handed it for — a pane opened from an
  // address bar has no page name, and asking for one would make a saved page the
  // price of looking at a picture. The refetch is the same read the corpus would
  // have been asked for anyway, and the endpoint has already written the bytes,
  // so the state is right however this request ends.
  //
  // Let go of only when the read that shows the files has landed, so the button
  // cannot be pressed again against a thread still being redrawn — and so
  // "fetching…" means the picture is coming, not that a request went out.
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

  // Where the pane lands: the newest entry, which is the message the list row
  // was a summary of (see newest). A thread is drawn oldest-first, because that is
  // what makes it readable as a transcript — but the row that was clicked
  // previewed the last message, and opening a five-screen trail to its top reads
  // as the email not being there at all. Landing on it is the pane keeping the
  // promise the row made, and the flash is so the arrival is visible on a thread
  // longer than the screen.
  //
  // Once per thread, and never again: a refetch redraws the open thread (saving
  // "who I am" invalidates it), and that must not drag a reader who has scrolled
  // back to the start away from where they were reading.
  const [landed, setLanded] = useState<string | null>(null);
  const landedFor = useRef<string | null>(null);
  // The corpus's people, by person id, as their addresses: read once for the whole
  // pane (see lib/who), because the participants panel and the reply box both join
  // against it. Here, among the hooks, rather than beside the titles it feeds — a
  // thread that is still loading renders three hooks fewer than a thread that has
  // arrived, and React counts them.
  const people = usePersonAddresses();
  // The transcript's behaviour over the pane's bubbles, the same module a built
  // page attaches (see Rendered): a chip the corpus holds bytes for opens in the
  // window over the pane rather than in a new tab, and a download asked for before
  // the bytes were here is replayed once they are. Above the early returns, since
  // a hook cannot be conditional on the thread having loaded.
  //
  // Re-attached when the entries are, because `attach` wires the elements it
  // finds, and the chips that arrive with bytes behind them (after a pull, after
  // saving "who I am") are elements the previous pass never saw.
  useEffect(() => {
    const detach = attach(document);
    return detach;
  }, [entries]);
  const target = newest(shown);
  useEffect(() => {
    if (!target || landedFor.current === thread.rootExtId) return;
    landedFor.current = thread.rootExtId;
    const el = document.getElementById(anchor(shown.indexOf(target)));
    // Absent in jsdom, and a landing that cannot scroll is still a landing: the
    // mark is what the reader sees either way.
    el?.scrollIntoView?.({ block: "start" });
    setLanded(target.extId);
  }, [thread.rootExtId, shown, target]);

  if (fetched.isError) return <Failure error={fetched.error} />;
  if (fetched.isPending) return <p className="selnote">Loading the thread…</p>;

  if (shown.length === 0) return <p className="selnote">No entries to show.</p>;

  // One colour rule, two callers: the same function the page build uses, over the
  // entries this pane was handed. A sender whose org nothing established takes the
  // stylesheet's unknown slot, which is what the page draws for them too.
  const slot = slotsFor(orgOrder(shown.map((e) => e.org)));
  // The hover title for a name, which is what the participants panel and the reply
  // box's audience line ask by. A sender is answered from their own entry's header,
  // which is evidence about that message; everybody else — a recipient, a cc, a
  // person whose only part in the thread is receiving it — has no address in the
  // read at all (see castOfEntries), so the corpus's identity graph answers instead.
  // The graph wins where it knows, because a title on a name is a claim about the
  // person: the entry's own careful "address unknown; quoted by …" is the wording
  // for the bubble, which is a claim about the message.
  const titles = new Map<string, string>();
  for (const e of shown) if (e.author) titles.set(e.author, senderTitle(e));
  for (const e of shown)
    for (const p of e.participants ?? []) {
      const addresses = people.get(p.personId);
      if (addresses) titles.set(p.name, withAddress(p.name, addresses));
    }
  // The provenance lines' own two lookups. A host is named by its mailbox id
  // where it has one — the same name a built page prints — and an unspooled id
  // links to this page's row for that message rather than out to the mailbox,
  // because the message it names is right here.
  //
  // Built from the DRAWN entries: a hoisted copy occupies no row, so a link to it
  // would land nowhere. A host that is one is named rather than linked, which is
  // what mailName already does with an id it cannot draw.
  const byExt = new Map<string, CorpusEntry>();
  const indexOf = new Map<string, number>();
  const anchorByGmail = new Map<string, string>();
  shown.forEach((e, i) => {
    byExt.set(e.extId, e);
    indexOf.set(e.extId, i);
    const gmail = gmailIdOf(e);
    if (gmail) anchorByGmail.set(gmail, anchor(i));
  });
  // Every entry the thread handed over, drawn or not. An edit names the copy the
  // quoter pasted, which is exactly the entry the hoist took out of the rows — so
  // the edit's own diff would have nothing to read if this lookup were the drawn
  // entries.
  const everyExt = new Map(entries.map((e) => [e.extId, e]));
  const mailName = (extId: string): string => {
    const host = byExt.get(extId);
    if (!host) return extId;
    const gmail = gmailIdOf(host);
    return gmail ? `msg ${gmail}` : host.extId;
  };

  // What this message answers, resolved against the thread the pane is holding:
  // the parent's own bubble is on this page, so the link lands on it rather than
  // asking the mailbox or the corpus for anything. Who and when are the parent's
  // — the name its head wears and the clock it states, which is the same clock
  // its own bubble prints, because both come from stampOf.
  //
  // A thread arrives whole from /v1/chains (the reply graph is walked server
  // side), so a parent that is not among these entries is a message the corpus
  // does not hold: an entry with no parent opens the chain, and the link says so
  // rather than guessing at what is missing.
  const replyOf = (e: CorpusEntry): ReplyTarget | null => {
    const parentId = parentOf.get(e.extId);
    const parent = parentId ? byExt.get(parentId) : undefined;
    if (!parent) return null;
    const i = indexOf.get(parent.extId);
    // Unreachable — the parent was reached through byExt, which is built from the
    // same walk — but a link with no anchor in it would be a worse answer than
    // saying the thread starts here.
    if (i === undefined) return null;
    const { who, whoTitle, when } = wordsOf(parent);
    return { anchor: anchor(i), who, whoTitle, when };
  };

  // A quoter's edit, resolved for this message's bubble by the same function the
  // page uses (see lib/edits). Who and when are this message's own — the person
  // who sent the quoting message is the person who made the edit — and the copy
  // the diff is made from is looked up among every entry, not only the drawn
  // ones, because that copy is the entry the hoist removed.
  //
  // The base comes back as an ext id, which is what the trail read has, and every
  // link on this page names a bubble the other way (see anchor). The base is one
  // of the drawn rows unless the message being quoted was itself an edited copy
  // of something — in which case it has been hoisted and there is no row to land
  // on, so the raw id stays and the link honestly points nowhere rather than at
  // somebody else's bubble.
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

  // The message a reply would answer: the newest entry of this thread that the
  // mailbox holds, which is the last thing said in it that can be answered.
  //
  // Newest rather than last drawn, because drawing the tree reorders bubbles without
  // reordering the conversation — a reply under an older message is still the
  // newest message. Answerable rather than simply newest, because the newest thing
  // here may be a line recovered from inside somebody else's quote: that has no
  // message in the mailbox to thread an answer onto, and a box that offered to
  // answer it would be offering a press the server must refuse.
  //
  // "The mailbox holds it" is read off the entry's own permalink (see gmailIdOf),
  // which is what every other mailbox-backed thing in this pane is conditional on
  // — the receipt line's link, the fetch button — and the server is the authority
  // either way: an entry it cannot answer is refused by name, and the box says so.
  //
  // Asked of the DRAWN entries, like every other link on this page: a hoisted
  // copy occupies no row (see the hoist above), so a box that offered to answer
  // one would be naming a message with no bubble to read it against.
  const answer = newest(shown.filter((e) => gmailIdOf(e) !== undefined));

  // The order the bubbles are drawn in and the replies that hang off each one:
  // the transcript's own order, or the reply tree when the pane's switch is on
  // (see lib/tree). Nothing about the entries changes — same corpus read,
  // same reply graph, same links — only which one comes next and which of the
  // tree's containers it is drawn inside, so a switch that is off is this pane
  // with every bubble at the top level and no container drawn at all.
  const forest = tree
    ? buildTree(shown, (e) => e.extId, (e) => parentOf.get(e.extId))
    : shown.map((entry) => ({ entry, replies: [] }));

  /** One bubble. The whole of what the switch changes is where this sits, so the
   *  element is named once and drawn the same way at every level and in both
   *  views — flat, every message is a tree of one with no replies. */
  const bubble = (e: CorpusEntry) => (
    <Message
          // The anchor is the entry's place in the thread as the corpus sent it,
          // not in the order it is drawn: the reply link under a bubble and the
          // id on a source line both name a message, and drawing the tree moves
          // bubbles
          // without moving what they are called. `indexOf` is the same map the
          // links are built from, so the two cannot disagree.
          id={anchor(indexOf.get(e.extId) ?? 0)}
          body={e.html ?? ""}
          sender={e.author}
          // Hovering the name (or the avatar) names the person fully: the address
          // the entry came from, as a page build's own title does. A recovered
          // entry has no address of its own, and then the title names that
          // absence and the person it was quoted by rather than borrowing an
          // address from the people table — the same rule the page follows, so
          // the two cannot name two addresses for one message.
          senderTitle={senderTitle(e)}
          // The sender's organisation as the corpus resolved it, on the slot this
          // thread's own first-appearance order gives it.
          orgSlot={slot(e.org)}
          // The reader's own message: the corpus resolved this entry's author
          // against the addresses the reader stored, so the pane makes no second
          // guess about whose mail this is. The mark itself is the page's — the
          // same `Message` with the same `me`, which is the class the stylesheet
          // already tints (`.msg.me .bub`). That is the decision the issue left
          // open, and it is settled this way on purpose: a pane-only vocabulary
          // for one fact — a border, an alignment — would be a second thing to
          // keep in step with what "sent by you" means on the page.
          me={e.mine}
          quoted={e.quoted}
          // The recipients the message itself stated, or nothing: a recovered
          // entry has no headers, and the line reads "to —" rather than naming
          // whoever the page guessed. The corpus makes this string with the same
          // function a page build does.
          to={e.to}
          // The receipt's names, by the same map the panel's rows and the reply
          // box's audience line use: the people on this line are mostly the ones
          // who sent nothing, so the thread read has no address for them and the
          // corpus's identity graph is where one comes from.
          toTitle={(name) => titles.get(name) ?? name}
          subject={e.subject}
          // Where this message was found, in the same receipt line a built page
          // prints under the same bubble — the message's mailbox id, or the hosts
          // a recovered one was unspooled from. The pane is a reader looking at
          // one thread and the page is a reader looking at a built one; the id in
          // the receipt is the thing both are holding in their hand.
          source={<Source source={sourceLine(e, mailName)} anchorByGmail={anchorByGmail} />}
          // What this message answers, as a built page prints it under the same
          // bubble: the arrow and the parent's name and clock, linking to the
          // parent's row here. The page resolves the parent through the spec's
          // rows and the pane through the entries it was handed — the same mark,
          // the same words, one component (see ReplyLink).
          reply={<ReplyLink parent={replyOf(e)} />}
          // A quoter's edit to a message this one quoted, drawn inside this bubble
          // exactly as a page build draws it — the same relation, the same diff,
          // the same component (see Edits and lib/edits). The derived copy it was
          // made against has been hoisted out of these rows, so this is the only
          // place the edit appears.
          edits={<Edits edits={editsOf(e, stampOf(e))} />}
          stamp={stampOf(e)}
          // The corpus entry as it arrived, so a bubble that renders wrong can be
          // pasted somewhere and read whole — the same affordance, and the same
          // button, the page offers.
          copyJson={e}
          // The sender's own html, where the corpus holds a part for this message:
          // `original` is the read's own answer to that (body_html is not empty),
          // so the control is drawn for the messages that can answer it and for no
          // others. The fetch is the app's one route to a message's own markup, and
          // it is this pane's to pass because the pane is a reader with a server
          // behind it — a built page has none and gets no control.
          original={e.original ? { extId: e.extId, load: fetchOriginal } : undefined}
          // The address the switch is kept against, so that pressing the control
          // on one of a sender's messages reads the rest of them the same way.
          // A recovered entry has no address of its own and then the switch is
          // kept against the message — see MessageProps.fromEmail. It is the
          // fallback now rather than the store: where the corpus resolved this
          // sender to a person, the answer is that person's.
          fromEmail={e.fromEmail}
          // Whose reading style this bubble draws, and the write that changes it.
          // Both go together or not at all: a person the corpus resolved this
          // sender to is what the answer is stored against, and the handle to
          // write it back with. An entry with nobody to hold the answer — one
          // recovered from a quote — gets neither, and its switch falls back to
          // this browser's own memory.
          person={
            e.personId ? { id: e.personId, preferOriginal: e.preferOriginal === true } : undefined
          }
          onPreferOriginal={
            e.personId ? (next: boolean) => flipStyle(e.personId!, next) : undefined
          }
          // The files this message carried, as the corpus read them. mediaBase is
          // the app's own route to stored bytes — the pane is a reader with a
          // server behind it, unlike a shared export, so a pulled file opens
          // here rather than sending the reader to the mailbox.
          attachments={e.attachments}
          // The corpus's handle for this entry, which is what a fetch button asks
          // for. The pane has it on every message it draws — a recovered entry
          // included, since that is an id the corpus minted for the row rather
          // than a mailbox id the message always had.
          extId={e.extId}
          // The fetch itself, on the same grant a built page's button presses.
          // Both are passed together or not at all: the button is offered only
          // where somebody can answer it (see Attachments).
          onPull={(extId) => {
            setPulling(extId);
            setPullNote(null);
            pull.mutate({ body: { entry: extId } });
          }}
          pulling={pulling}
          mediaBase={MEDIA_BASE}
          // The message the pane landed on, flashed once and then let go. Only
          // that one message is handed the end-of-flash callback: a thread is one
          // landing, and every other bubble has no mark to take off.
          landed={e.extId === landed}
          onLandedEnd={e.extId === landed ? () => setLanded(null) : undefined}
        />
  );

  /** A message and, underneath it, the replies to it in one container of their
   *  own: the container is the indent and its own left border is the line beside
   *  them (see .ibread .stream .replies), which is how unroller draws its tree.
   *  One container per message rather than a number per bubble is what makes a
   *  level's line run unbroken from the first answer to the last and stop where
   *  the subtree does. A message with no replies draws no container at all, so a
   *  thread with no graph in it is drawn exactly as the flat view draws it. */
  const draw = (node: Knot<CorpusEntry>): ReactNode => (
    <Fragment key={node.entry.extId}>
      {bubble(node.entry)}
      {node.replies.length ? (
        <div className="replies">{node.replies.map((n) => draw(n))}</div>
      ) : null}
    </Fragment>
  );

  return (
    <div className="stream">
      {pullNote ? (
        <p className="pullnote" role="status">
          {pullNote}
        </p>
      ) : null}
      {/* Who is in the thread, over the messages themselves — a page built from the
          same thread opens with the same panel, and both are read the same way.
          It is here rather than in the pane's head because the head is one line of
          the message the reader is on, and a cast of fifteen is a block: in the
          stream it scrolls away with the thread, and the scroll position a reader
          had is the same as it was. The panel is collapsible, and this is why the
          summary line is a thing to press. */}
      <ParticipantsPanel
        // One row per message, which is what the panel counts people by and how it
        // finds their faces; the same slots the bubbles are coloured on, so a
        // person's row and their messages cannot disagree.
        v={{
          rows: shown.map((e) => ({
            entry: { sender: e.author, org: e.org, fromEmail: e.fromEmail },
          })),
          orgSlot: slot,
          // The bubbles' own hover title, by name rather than by entry: the panel
          // holds people, and the entry it was reached through is not the one the
          // reader is pointing at.
          whoTitle: (name: string) => titles.get(name) ?? name,
        }}
        people={castOfEntries(shown)}
      />
      {forest.map((node) => draw(node))}
      {/* The reply box, under everything said so far: the newest thing on this
          screen is the message it answers, and an answer belongs at the bottom of
          the conversation it continues. It is inside the pane's scroll box with
          the bubbles rather than pinned below it — the reader's own words are
          typed here and read back here, and a control that floats over a long
          thread would hide the message being quoted while it is being read.

          Drawn only when the thread holds something the mailbox can answer: a
          trail of recovered quotes and Slack posts has no message to thread a
          reply onto, and a composer there could only offer a press that fails. */}
      {answer ? (
        <ReplyBox thread={thread} answer={answer} words={wordsOf(answer)} />
      ) : null}
    </div>
  );
}
