import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError, $api, type CorpusEntry } from "../lib/api";
import { MEDIA_BASE, pullSummary } from "../lib/attachments";
import { attach } from "../client/behaviour";
import { orgOrder, slotsFor } from "../lib/derive";
import { newest } from "../lib/newest";
import { gmailIdOf, sourceLine } from "../lib/sources";
import { fetchOriginal } from "../lib/original";
import { Failure } from "./ThreadPreview";
import { Message, type StampData } from "./Message";
import { ParticipantsPanel, castOfEntries } from "./Participants";
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

/** What hovering the sender says: their name and the address the mail came from,
 *  e.g. "Lane Whittaker <lane@whittaker.example>". The same string a page build
 *  makes for the same entry (see derive.ts's whoTitle), because a reader reading
 *  one thread in two places should be told the same thing about it.
 *
 *  A message recovered from inside someone else's quote has no From header of its
 *  own, so there is no address to hang on the name — and the corpus will not lend
 *  one, because an address reached by matching the sender's name is not evidence
 *  about who sent this. The absence is named instead, with the address that is
 *  real here and labelled as what it is: the quoter's. Silence would read as the
 *  pane failing to fill in what it fills in on every other bubble in the thread.
 *
 *  The page answers a different question in its own source line, and should keep
 *  doing so: "unspooled from msg g-a" is about where the text came from, and is
 *  printed under every bubble whether or not anyone can be named. A hover is
 *  about the person, so it says which person's address this is rather than where
 *  the entry was found. */
function senderTitle(e: CorpusEntry): string {
  const name = e.author ?? "";
  if (!e.fromEmail) {
    if (!e.fromQuotedBy) return name;
    // Nothing to hang the parenthesis on when the entry has no name either, so
    // it stands alone rather than starting with a space and a bracket.
    const unknown = `address unknown; quoted by ${e.fromQuotedBy}`;
    return name ? `${name} (${unknown})` : unknown;
  }
  if (!name) return e.fromEmail;
  return `${name} <${e.fromEmail}>`;
}


export function ThreadMessages({ thread }: { thread: { rootExtId: string } }) {
  const queryClient = useQueryClient();
  const fetched = $api.useQuery("get", "/v1/chains/{rootExtId}", {
    params: { path: { rootExtId: thread.rootExtId } },
  });

  const entries = fetched.data?.entries ?? [];

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
  const target = newest(entries);
  useEffect(() => {
    if (!target || landedFor.current === thread.rootExtId) return;
    landedFor.current = thread.rootExtId;
    const el = document.getElementById(anchor(entries.indexOf(target)));
    // Absent in jsdom, and a landing that cannot scroll is still a landing: the
    // mark is what the reader sees either way.
    el?.scrollIntoView?.({ block: "start" });
    setLanded(target.extId);
  }, [thread.rootExtId, entries, target]);

  if (fetched.isError) return <Failure error={fetched.error} />;
  if (fetched.isPending) return <p className="selnote">Loading the thread…</p>;

  if (entries.length === 0) return <p className="selnote">No entries to show.</p>;

  // One colour rule, two callers: the same function the page build uses, over the
  // entries this pane was handed. A sender whose org nothing established takes the
  // stylesheet's unknown slot, which is what the page draws for them too.
  const slot = slotsFor(orgOrder(entries.map((e) => e.org)));
  const titles = new Map<string, string>();
  for (const e of entries) if (e.author) titles.set(e.author, senderTitle(e));
  // The provenance lines' own two lookups. A host is named by its mailbox id
  // where it has one — the same name a built page prints — and an unspooled id
  // links to this page's row for that message rather than out to the mailbox,
  // because the message it names is right here.
  const byExt = new Map<string, CorpusEntry>();
  const anchorByGmail = new Map<string, string>();
  entries.forEach((e, i) => {
    byExt.set(e.extId, e);
    const gmail = gmailIdOf(e);
    if (gmail) anchorByGmail.set(gmail, anchor(i));
  });
  const mailName = (extId: string): string => {
    const host = byExt.get(extId);
    if (!host) return extId;
    const gmail = gmailIdOf(host);
    return gmail ? `msg ${gmail}` : host.extId;
  };

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
          rows: entries.map((e) => ({
            entry: { sender: e.author, org: e.org, fromEmail: e.fromEmail },
          })),
          orgSlot: slot,
          // The bubbles' own hover title, by name rather than by entry: the panel
          // holds people, and the entry it was reached through is not the one the
          // reader is pointing at.
          whoTitle: (name: string) => titles.get(name) ?? name,
        }}
        people={castOfEntries(entries)}
      />
      {entries.map((e, i) => (
        <Message
          key={e.extId}
          id={anchor(i)}
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
          subject={e.subject}
          // Where this message was found, in the same receipt line a built page
          // prints under the same bubble — the message's mailbox id, or the hosts
          // a recovered one was unspooled from. The pane is a reader looking at
          // one thread and the page is a reader looking at a built one; the id in
          // the receipt is the thing both are holding in their hand.
          source={<Source source={sourceLine(e, mailName)} anchorByGmail={anchorByGmail} />}
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
      ))}
    </div>
  );
}
