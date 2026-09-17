import { useEffect, useRef, useState } from "react";
import type { CSSProperties, ReactNode } from "react";
import { initials } from "../lib/anchors";
import {
  attHref,
  hasPreview,
  isSkipped,
  localHref,
  skipNote,
  type Attachment,
} from "../lib/attachments";
import type { ZoneState } from "../lib/chronological";
import { mountOriginal } from "../lib/original";
import { trimBody } from "../lib/trimBody";

/**
 * Message — one message of a transcript, drawn from data alone.
 *
 * The presentation half of the transcript. Everything it needs arrives as a
 * prop: the body is already the HTML the reader will see, the org colour has
 * already been resolved to a slot class ("o2"), and the transcript's layout
 * arrives as the grid position to apply rather than as something to work out.
 * Nothing here imports `Row` or `View`, so a bubble can be drawn by any caller
 * that has a sender, a clock and a body — the timeline is only the first one.
 *
 * The rule the split is built on is what a piece needs, not whether it is
 * optional:
 *
 *   - What the caller decides, passed as data: the clock and how much of it is
 *     a claim, the "to" line, the payload behind the copy button.
 *   - What only the pipeline can produce, passed as an optional slot (a
 *     rendered node): the reply link (which resolves a parent through the reply
 *     graph), the provenance line (which resolves ids to anchors on this page),
 *     and a quoter's inline edit (which resolves a diff against the message it
 *     was made to). Nodes rather than spec types, so this file stays ignorant of
 *     both the spec and `derive`.
 *
 * An absent slot renders nothing, so a caller that has none of that furniture
 * gets a plain bubble — and the timeline, which passes all of it, renders the
 * DOM it rendered before the split.
 */

const html = (s: string) => ({ __html: s });

/** What the header's timestamp needs: the clock as written, and how much of it
 *  is the page's own claim. No `Row` — a caller holding a date, a time and a
 *  zone has everything this asks for. */
export interface StampData {
  /** as displayed, e.g. "Thu 16 Jul 2026" */
  date: string;
  /** as displayed, e.g. "11:35"; absent for an entry with no clock */
  time?: string;
  /** the zone label, e.g. "AEDT"; absent when nothing placed it */
  tz?: string;
  /** how much the page may claim about `tz` */
  zone: ZoneState;
}

export interface MessageProps {
  /** the bubble's anchor id; the timestamp links to it */
  id: string;
  /** presentation HTML, already sanitised; its edges are trimmed here, because
   *  which whitespace the reader must not see is a rendering question */
  body: string;
  /** the sender as displayed; absent on a message with no name on it */
  sender?: string;
  /** the message's own subject, as the message stated it; absent on a message
   *  that carried none (a recovered entry, a note). Drawn in the header's
   *  receipt rather than on the header line: the line is who and when, and a
   *  subject there would be a second title under the thread's own — but a reply
   *  that renames a thread is otherwise unrecoverable, so each message states
   *  the one it had where the reader has gone to read *this* message. */
  subject?: string;
  /** what hovering the sender says, e.g. "Ada Okoye <ada@example.com>"; absent
   *  falls back to the name */
  senderTitle?: string;
  org?: string;
  /** the org's colour slot, e.g. "o2". It rides on the bubble, not only on the
   *  avatar: a page is scanned in the body column, so the colour has to be
   *  where the eye already is. */
  orgSlot: string;
  /** the sender's avatar image class, e.g. "p0"; absent draws their initials */
  avatarClass?: string;
  /** the reader's own outbound */
  me?: boolean;
  /** reconstructed from quoted text; drawn dashed, since the page did not
   *  receive it as a standalone message */
  quoted?: boolean;
  /** the message the pane landed on when this thread opened: the newest, which is
   *  what the row that was clicked was a summary of. Drawn as a one-shot flash
   *  (see .msg.landed), because landing is an arrival rather than a state the
   *  message is in. */
  landed?: boolean;
  /** called when that flash finishes, so the caller can take the mark off. */
  onLandedEnd?: () => void;
  /** people @-named in the body, shown above it */
  mentions?: string[];
  attachments?: Attachment[];
  /** the corpus's handle for this message, which the fetch button asks for */
  extId?: string;
  /** fetch this message's files, where a host will do it at all */
  onPull?: (extId: string) => void;
  /** the message whose files are being fetched, so its button can say so */
  pulling?: string | null;
  /** where the corpus serves stored bytes; empty in the static export, which has
   *  no server to serve them from */
  mediaBase?: string;
  /** as it appeared on the message, e.g. "Bo Halvorsen, cc …"; absent reads "—" */
  to?: string;
  stamp: StampData;
  /** where the bubble sits in the transcript grid, from the layout pass */
  style?: CSSProperties;
  /** thread-column index, for the client's column view */
  lane?: number;
  /** opens its thread; marked where the columns are shown */
  chainStart?: boolean;
  /** what changed since a previous render, where there was one */
  mark?: "new" | "revised";
  /** the reply relationship — a node, because only the pipeline knows how a
   *  message resolves the parent it replies to. Drawn at the right of the
   *  header line, beside the caret, where the transcript is scanned. */
  reply?: ReactNode;
  /** where the entry was found — the ids under it, in the header's expanded
   *  section beside the to/cc line */
  source?: ReactNode;
  /** a quoter's inline edit to text this message quoted */
  edits?: ReactNode;
  /** what the clip button puts on the clipboard as JSON; absent leaves the
   *  button off, since a button that copies nothing is a lie. The button rides
   *  in the header's expanded section, with the rest of the receipt. */
  copyJson?: unknown;
  /** The sender's own html for this message, and how to fetch it: passed exactly
   *  where a caller knows the corpus holds a part for the entry (the `original`
   *  flag on a chain or entry read) and has a server to fetch it from. So what is
   *  absent here is absent everywhere — on a page rendered to a file, in a static
   *  export, on a message that arrived as plain text — and no control is drawn,
   *  because a control that cannot answer is worse than no control. */
  original?: { extId: string; load: (extId: string) => Promise<string> };
}

/** The sender's face: their avatar image where the page has one, their initials
 *  otherwise. Decoration — the name beside it is what names them. */
function Avatar({ name, orgSlot, pic, title }: {
  name: string;
  orgSlot: string;
  /** this sender's avatar image class, absent when they have no picture */
  pic?: string;
  title?: string;
}) {
  return (
    <div className={`av ${orgSlot}${pic ? ` pic ${pic}` : ""}`} title={title}>
      {pic ? null : <span className="ini">{initials(name)}</span>}
    </div>
  );
}

/**
 * A clip button that drops the payload it is handed onto the clipboard as JSON.
 *
 * Quiet, and not a navigational control: inline handlers only, no listener of
 * its own. The payload is the caller's business — the timeline hands it the spec
 * entry as the renderer saw it, so a message that renders wrong can be pasted
 * somewhere and inspected whole; all this end knows is that it is JSON.
 */
function CopyJson({ data }: { data: unknown }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      className="copyjson"
      title="Copy this message's JSON"
      aria-label="Copy this message's JSON"
      onClick={() => {
        navigator.clipboard?.writeText(JSON.stringify(data, null, 2)).then(
          () => setDone(true),
          () => {},
        );
        window.setTimeout(() => setDone(false), 1200);
      }}
    >
      {done ? "copied" : (
        <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true">
          <rect x="5.5" y="5.5" width="8" height="8" rx="1.2" fill="none"
            stroke="currentColor" strokeWidth="1.4" />
          <path d="M3 10.5 V3.5 a.5.5 0 0 1 .5-.5 H10" fill="none"
            stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
        </svg>
      )}
    </button>
  );
}

/**
 * A zone is shown three ways, because the reader's next move differs in each.
 * Stated is a fact and reads as one. Inferred is a claim and is dotted, dimmed
 * and suffixed so it cannot be mistaken for the source's own words. Unknown is
 * neither, and is marked with a bare "?" rather than left as whitespace — an
 * unlabelled clock beside a labelled one silently invites the reader to compare
 * them, and on this page most clocks are unlabelled. The mark is the whole of it:
 * "zone unknown" at every held-back message is a sentence the reader learns to
 * skip, and the tooltip still says why.
 */
function Stamp({ id, stamp }: { id: string; stamp: StampData }) {
  const { date, time, tz, zone } = stamp;
  return (
    <a className="tm pl" href={`#${id}`} title="Link to this message">
      {date}
      {time ? ` · ${time}` : ""}
      {zone === "stated" ? <span className="tz">{tz}</span> : null}
      {zone === "inferred" ? (
        <span
          className="tz tzi"
          title="Inferred — this source stated no zone. The offset was worked out from the client that quoted this message; see the source notes."
        >{` ${tz}?`}</span>
      ) : null}
      {zone === "unknown" ? (
        <span
          className="tz tzu"
          title="Zone unknown — this source stated none and nothing available places it. The clock is a wall clock as quoted, so it cannot be compared with the times above and below it."
        >
          {" ?"}
        </span>
      ) : null}
    </a>
  );
}

/** The attachment strip: a link to where a file already is, and — where it is not
 *  here yet and somebody can go and get it — the download itself.
 *
 *  It reads an attachment list rather than an entry, because nothing below needs
 *  anything else the entry carries — the one handle it does need, `extId`, is
 *  the message's own and is passed as itself.
 */
function Attachments({ attachments = [], extId, onPull, pulling, mediaBase }: {
  attachments?: Attachment[];
  /** the handle a download asks for: the message whose files it wants */
  extId?: string;
  /** fetch this message's files, where a host will do it at all */
  onPull?: (extId: string) => void;
  /** the message whose files are being fetched, so its chips can say so */
  pulling?: string | null;
  /** where the corpus serves stored bytes; empty in the static export, which has no server */
  mediaBase?: string;
}) {
  if (!attachments.length) return null;
  // Whether THIS message's files are being fetched. A message is the unit the
  // endpoint works in — one mailbox round trip, and every file it carried — so
  // one press puts every chip on the line into the same state.
  const fetching = pulling != null && pulling === extId;
  return (
    <div className="atts">
      <span className="clip">attached</span>
      {attachments.map((a, i) => {
        const local = localHref(a, mediaBase ?? "");
        const href = attHref(a, mediaBase);
        // Whether this chip can go and get the file. It is offered only where
        // there is something to fetch and somebody able to fetch it: a host
        // started without -media never passes onPull, a page rendered to a file
        // never does, and a message whose bytes are already here has nothing to
        // ask for. A file the corpus has DECLINED cannot be asked for again —
        // the reason is recorded, not the answer — so it stays the plain link to
        // its source that every chip used to be.
        const fetchable = !local && !isSkipped(a) && onPull !== undefined && extId !== undefined;
       const thumb = hasPreview(a) ? (
          <img
            className="athumb"
            src={a.preview}
            width={a.previewW}
            height={a.previewH}
            /* Decorative here: the filename beside it already names the file, so
               announcing it twice only makes the chip longer to listen to. */
            alt=""
          />
        ) : null;
        const label = (
          <>
            {thumb}
            <span className="afn">{a.name}</span>
            <span className="ameta">
              {fetching && fetchable ? (
                <>
                  {/* The same ↻ and the same 0.8s turn the nav's refresh wears
                      (see .navrefresh .spinner): one glyph for "this is being
                      worked on", in both the places this app asks a server for
                      something that takes a moment. Decorative — the word beside
                      it says the same thing to a reader who cannot see it turn. */}
                  <span className="spinner" aria-hidden="true" />
                  downloading…
                </>
              ) : (
                <>
                  {a.kind ?? "file"} · {a.size ?? ""}
                </>
              )}
            </span>
          </>
        );
        // What shows these bytes in the window over the page, when anything can:
        // the preview the builder embedded — a picture by definition — or the
        // server's `view` for bytes this host holds. The preview comes first
        // because a thumbnail from the archive has a picture here and no bytes of
        // ours to fetch.
        //
        // `view` is asked separately from `open` and not derived from it, because
        // they answer different questions: `open` is what a click does with the
        // link, `view` is what the window contains. The PDF is where they differ
        // — a click takes it (Content-Disposition `attachment`) and the window
        // still frames it. A file with no view can only be taken: an archive, a
        // document, markup (a frame is a document, so a sender's HTML framed here
        // would be script in our origin), and media, which plays in a tab.
        // A picture is a picture whether or not the spec that carried it knew the
        // word for one: a page saved before `view` existed still holds bytes and a
        // kind, and re-deriving it must not be the price of enlarging something.
        // Everything else is the server's `view`, which a fresh derivation
        // carries and an old one cannot — so a PDF in an old page stays a chip
        // until the page is rebuilt.
        const showsImage = thumb !== null || (Boolean(local) && a.kind === "image");
        const view = showsImage ? "image" : Boolean(local) ? a.view ?? "" : "";
        const opens = view !== "";
        // The chip stays the same link it always was, and the popover is layered
        // onto it by script. That is deliberate: no new control appears, the
        // trigger is already in the tab order, and with scripting unavailable
        // the click still opens the attachment rather than doing nothing.
        //
        // Once the bytes are local, where the click goes changes but the shape
        // does not: the chip is still one link, and the server decides whether it
        // downloads or opens by the same `open` rule. A file the reader is TAKING
        // opens in no tab at all — the browser saves it and the page stays put,
        // which is what a download should do. One they are LOOKING at opens
        // beside the page, so the transcript is still there behind it.
        const beside = !local || a.open !== "download";
        // A file the corpus has refused says so where the reader is already
        // looking, rather than in a console they will not open. It is a tooltip
        // and not more chip text: the chip is a list of files, and a sentence in
        // the middle of it would push the next file off the line.
        const note = skipNote(a);
        // What a chip says when the pointer is on it. The skip reason first, since
        // a chip that cannot be fetched is the one whose state is not visible from
        // the outside; then what a press will do, because the href under the chip
        // names the mailbox and the press is not going there.
        const tip =
          note ??
          (fetchable
            ? fetching
              ? "Downloading this file from the mailbox…"
              : "Download this file from the mailbox"
            : undefined);
        return href ? (
          <a
            key={i}
            className={["att", opens && "haspop", fetching && fetchable && "busy"]
              .filter(Boolean).join(" ")}
            href={href}
            {...(tip ? { title: tip } : {})}
            {...(beside ? { target: "_blank", rel: "noopener" } : {})}
            {...(opens
              ? {
                  "data-pop": a.name,
                  "data-view": view,
                  "aria-haspopup": "dialog" as const,
                }
              : {})}
            /* The popover's save control reads this, not the href: a Slack chip's
               href is a permalink and a body picture has no href at all, so the
               presence of the local URL is what says "these bytes are here". */
            {...(local ? { "data-get": local } : {})}
            /* The download itself, on a chip whose file is not here yet: the press
               asks the host for this message's files, and the request is marked on
               the element so the click can be replayed the moment there are bytes
               behind the chip (see behaviour.ts). That is what makes the press a
               download rather than a promise — press, wait, and the file opens.

               A modified press is left alone: it is the reader asking for a new tab
               or a download of the link *under* the chip, which is where the file
               is today, and the link is the right answer to that.

               The marker is set on the element rather than rendered from state,
               because the render that follows the pull is the one that has to find
               it — state would have to survive a props change that replaces every
               attachment, and a DOM attribute does that by being there. */
            {...(fetchable
              ? {
                  onClick: (ev: React.MouseEvent<HTMLAnchorElement>) => {
                    if (ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.altKey || ev.button !== 0)
                      return;
                    ev.preventDefault();
                    // Already asked for, and the answer is on its way: a second
                    // press would spend a second mailbox round trip on files that
                    // are already coming.
                    if (fetching) return;
                    ev.currentTarget.setAttribute("data-download", "");
                    onPull!(extId!);
                  },
                }
              : {})}
            {...(fetching && fetchable ? { "aria-busy": true } : {})}
          >
            {label}
          </a>
        ) : (
          // A thumbnail still earns its place on a chip with nowhere to go — it
          // is the only thing here that says what the file actually is. It gets
          // no popover, though: the only way to offer one would be a control
          // that does nothing at all without scripting.
          <span key={i} className="att nolink" {...(note ? { title: note } : {})}>
            {label}
          </span>
        );
      })}
    </div>
  );
}

/**
 * The reader's second reading of one message.
 *
 * By default a body is the transcript's: the sender's markup as this pipeline
 * renders it, stripped of the stylesheet and the class names that made it what it
 * was, so that a page of other people's design reads as one page. That stripping
 * is what makes some mail illegible — a calendar invite is tables and classes and
 * nothing else, and a newsletter's white text loses the background it was white
 * against along with the rule that set it. Where the corpus holds the sender's own
 * part, the reader can ask for it instead, and it is mounted in a shadow root of
 * its own so nothing in it can reach the app and nothing in the app can reach it.
 *
 * It is swapped and not added. Two renditions stacked would be twice the height of
 * a thread to hold a comparison the reader has already made by the time they reach
 * for the control — and the whole reason to reach for it is that one of the two is
 * wrong for this message.
 *
 * The state lives here, at the bubble, rather than in either end of the swap: the
 * control belongs in the receipt (where the reader goes to inspect a message) and
 * the body it replaces belongs in the bubble, and one of them cannot own the other
 * without the other reaching for it. The control is drawn only where a caller
 * passed somewhere to fetch from, so a built page and a static export never show
 * one — see MessageProps.original.
 */
function useOriginal(original?: MessageProps["original"]) {
  const [state, setState] = useState<Original>({ at: "read" });
  // What arrived, held apart from what is on screen: the reader who flips back and
  // forth is comparing two renderings of one body, and re-asking for bytes this
  // bubble already has would make the comparison a round trip each way. lib/original
  // holds the same answer for the whole session (a second bubble for the same
  // message, or one remounted, pays nothing either) — this is what keeps the flip
  // itself free.
  const arrived = useRef<string | null>(null);

  const ask = () => {
    if (!original) return;
    if (state.at === "sent") {
      // Back to the transcript: the bytes stay in `arrived`, so coming back to
      // them costs nothing.
      setState({ at: "read" });
      return;
    }
    if (state.at !== "read") return;
    if (arrived.current !== null) {
      setState({ at: "sent", html: arrived.current });
      return;
    }
    setState({ at: "asking" });
    original.load(original.extId).then(
      (html) => {
        arrived.current = html;
        setState({ at: "sent", html });
      },
      (err: unknown) =>
        setState({
          at: "none",
          why: err instanceof Error && err.message ? err.message : "the original is not available",
        }),
    );
  };

  return { state, ask };
}

/**
 * The control, in the bubble's receipt beside the copy button.
 *
 * The receipt is where a reader goes to inspect a message rather than read it: the
 * ids it was found under, the address it was sent to, the JSON behind it. This asks
 * for the same message a second way, so it belongs with those rather than on the
 * bubble — as chrome over the body it would be a control on every message that had
 * one (most of them), while the reader who needs it is the one who has already
 * noticed the rendering is wrong.
 *
 * `none` is the one answer that leaves nothing to press: the corpus was asked and
 * said there is nothing of the sender's to show. The server's own sentence is the
 * answer, so it rides the note's title rather than being replaced with a word.
 */
function OriginalControl({ state, ask }: { state: Original; ask: () => void }) {
  if (state.at === "none") {
    return (
      <span className="origwhy" title={state.why}>
        nothing to show
      </span>
    );
  }
  return (
    <button
      type="button"
      className="origbtn"
      aria-pressed={state.at === "sent"}
      disabled={state.at === "asking"}
      title={
        state.at === "sent"
          ? "Back to the rendered body: the same message with the sender's own styling removed"
          : "Show this message as it was written: the sender's own markup and its own stylesheet, in a shadow root, which is where the stylesheet cannot reach this page"
      }
      onClick={ask}
    >
      {state.at === "asking" ? "loading…" : "original"}
    </button>
  );
}

/**
 * The bubble's body, in whichever of the two renderings the reader last asked
 * for. The mount is the only imperative thing in this file: a shadow root is DOM
 * rather than React, and the element it is created on is created by the render.
 */
function Body({ body, state }: { body: string; state: Original }) {
  const host = useRef<HTMLDivElement | null>(null);
  // Mounted by effect rather than in the ref callback: the element exists on the
  // render that swaps to it, and the shadow root is created on the element as
  // part of that commit. React owns the host as an empty div; the mail is written
  // into it by this one call, and never read back.
  useEffect(() => {
    if (state.at === "sent" && host.current) mountOriginal(host.current, state.html);
  }, [state]);

  if (state.at === "sent") {
    /* The shadow host. Empty as far as React is concerned — the mail is written
       into its shadow root, where a rule of this page's cannot reach it and its
       own rules cannot leave. */
    return <div className="bd bdo" ref={host} />;
  }
  return <div className="bd" dangerouslySetInnerHTML={html(trimBody(body))} />;
}

/** What the reader has asked for, and what came back.
 *
 * `read` is the transcript's own rendering and the state a bubble opens in;
 * `asking` is a fetch in flight, during which the transcript's rendering is
 * still what is on screen, because taking the body away before the replacement
 * arrives would be a blank bubble on a slow corpus. `sent` is the sender's own
 * html, mounted. `none` is the corpus's answer that there is nothing of theirs
 * to show — an answer rather than a failure, and the only state with no way back
 * to `read` except reloading the thread, because there is nothing to go back to:
 * what was on screen is still on screen. */
type Original =
  | { at: "read" }
  | { at: "asking" }
  | { at: "sent"; html: string }
  | { at: "none"; why: string };

/** One message bubble. */
export function Message(p: MessageProps) {
  // The org slot rides on the bubble so a bubble can carry its sender's colour,
  // and `me`/`quoted`/`isnew` are the same colour-and-state modifiers the
  // stylesheet already reads off this element.
  const cls = ["msg", p.orgSlot, p.me && "me", p.quoted && "q",
    p.chainStart && "chstart", p.mark === "new" && "isnew", p.landed && "landed"]
    .filter(Boolean)
    .join(" ");
  // The receipt's controls are the last thing on the line, and the two of them
  // sit together: the tail of the receipt is where a reader inspects the message
  // rather than reads it, and a second way to read it belongs beside the way to
  // copy it, not on the bubble.
  const original = useOriginal(p.original);
  // The name's hover title, and the avatar's: both name the person the same way,
  // and a caller that supplies no title gets the name it already gave us.
  const who = p.senderTitle ?? p.sender ?? "";
  return (
    <div
      className={cls}
      id={p.id}
      data-ch={p.lane}
      style={p.style}
      // The flash is on the bubble (see .msg.landed), so it is the bubble's
      // animation end that reaches this handler; the name is checked because an
      // entry with any other animation must not clear a mark that is still
      // running.
      onAnimationEnd={(e) => {
        if (e.animationName === "flash") p.onLandedEnd?.();
      }}
    >
      <div className="col">
        {/* The header is the bubble's disclosure, not a caption: the sender, the
            org, the clock and the reply the message answers are what a page is
            scanned by, and the receipt — who it was addressed to, the ids it was
            found under, and the control that copies it whole — opens beneath
            rather than sitting inside the bubble, where it is read once and is
            only height thereafter. A native <details>, like the provenance line
            and the panels: the export stays readable without scripting, and
            find-in-page reaches the ids closed or open. */}
        <details className="hdr">
          <summary>
            <Avatar name={p.sender ?? ""} orgSlot={p.orgSlot} pic={p.avatarClass} title={who} />
            <span className="nm" title={who}>
              {p.sender}
            </span>
            <span className="org">{p.org}</span>
            <Stamp id={p.id} stamp={p.stamp} />
            {p.mark === "new" ? <span className="newpill">new</span> : null}
            {p.mark === "revised" ? <span className="revpill">revised</span> : null}
            {/* The line's right end, and always drawn even when the caller has
                no reply to put in it: the caret lives inside this box, so an
                empty tail still closes the line at the right edge. */}
            <span className="htail">{p.reply}</span>
          </summary>
          <div className="hdet">
            {/* The subject, on its own line above the receipt's fields, and only
                where the message had one: a message with no subject has nothing
                to say here, and an empty line would say it anyway. */}
            {p.subject ? (
              <span className="subj" title={p.subject}>
                {p.subject}
              </span>
            ) : null}
            <span className="to">to {p.to ?? "—"}</span>
            {p.source}
            {p.original !== undefined || p.copyJson !== undefined ? (
              <span className="hdetend">
                {p.original !== undefined ? (
                  <OriginalControl state={original.state} ask={original.ask} />
                ) : null}
                {p.copyJson !== undefined ? <CopyJson data={p.copyJson} /> : null}
              </span>
            ) : null}
          </div>
        </details>
        <div className="bub">
          {p.mentions?.length ? (
            <div className="ment">
              {p.mentions.map((m) => (
                <span className="at" key={m}>
                  @{m}
                </span>
              ))}
            </div>
          ) : null}
          {/* The body, and the second reading of it: this is where the sender's own
              html is mounted in place of the rendered one. Which one to draw is
              the receipt's control's state, held at the bubble above. */}
          <Body body={p.body} state={original.state} />
          {p.edits}
          <Attachments
            attachments={p.attachments}
            extId={p.extId}
            onPull={p.onPull}
            pulling={p.pulling}
            mediaBase={p.mediaBase}
          />
        </div>
      </div>
    </div>
  );
}
