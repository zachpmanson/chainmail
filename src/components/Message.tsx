import { useState } from "react";
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
  /** chain-column index, for the client's column view */
  lane?: number;
  /** opens its chain; marked where the columns are shown */
  chainStart?: boolean;
  /** what changed since a previous render, where there was one */
  mark?: "new" | "revised";
  /** the reply relationship — a node, because only the pipeline knows how a
   *  message resolves the parent it replies to */
  reply?: ReactNode;
  /** the provenance line under the bubble */
  source?: ReactNode;
  /** a quoter's inline edit to text this message quoted */
  edits?: ReactNode;
  /** what the clip button puts on the clipboard as JSON; absent leaves the
   *  button off, since a button that copies nothing is a lie */
  copyJson?: unknown;
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

/** The attachment strip: a link to where a file already is, or a fetch button
 *  where it is not here yet.
 *
 *  It reads an attachment list rather than an entry, because nothing below needs
 *  anything else the entry carries — the one handle it does need, `extId`, is
 *  the message's own and is passed as itself.
 */
function Attachments({ attachments = [], extId, onPull, pulling, mediaBase }: {
  attachments?: Attachment[];
  /** the handle the fetch button asks for */
  extId?: string;
  /** fetch this message's files, where a host will do it at all */
  onPull?: (extId: string) => void;
  /** the message whose files are being fetched, so its button can say so */
  pulling?: string | null;
  /** where the corpus serves stored bytes; empty in the static export, which has no server */
  mediaBase?: string;
}) {
  if (!attachments.length) return null;
  // The button is offered only where there is something to fetch and somebody
  // able to fetch it: a host started without -media never passes onPull, a page
  // rendered to a file never does, and a message whose files are all already in
  // the corpus is done with the question. A file the corpus has DECLINED is done
  // with it too — the reason is recorded, not the answer — so it cannot keep the
  // button alive on a message that has nothing left to ask for.
  const pending = attachments.some((a) => !a.blobSha && !isSkipped(a));
  return (
    <div className="atts">
      <span className="clip">attached</span>
      {attachments.map((a, i) => {
        const local = localHref(a, mediaBase ?? "");
        const href = attHref(a, mediaBase);
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
              {a.kind ?? "file"} · {a.size ?? ""}
            </span>
          </>
        );
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
        return href ? (
          <a
            key={i}
            className={thumb ? "att haspop" : "att"}
            href={href}
            {...(note ? { title: note } : {})}
            {...(beside ? { target: "_blank", rel: "noopener" } : {})}
            {...(thumb
              ? { "data-pop": a.name, "aria-haspopup": "dialog" as const }
              : {})}
            /* The popover's save control reads this, not the href: a Slack chip's
               href is a permalink and a body picture has no href at all, so the
               presence of the local URL is what says "these bytes are here". */
            {...(local ? { "data-get": local } : {})}
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
      {onPull && pending && extId ? (
        // A button, not a chip: a chip goes to where the file already is, and
        // this one goes and gets it. Plain text, because the row is already a
        // run of framed chips and a second frame would read as another file.
        <button
          type="button"
          className="attget"
          disabled={pulling != null}
          title="Fetch this message's attached files into the corpus, so they can be shown here"
          onClick={() => onPull(extId)}
        >
          {pulling === extId ? "fetching…" : "fetch files"}
        </button>
      ) : null}
    </div>
  );
}

/** One message bubble. */
export function Message(p: MessageProps) {
  // The org slot rides on the bubble so a bubble can carry its sender's colour,
  // and `me`/`quoted`/`isnew` are the same colour-and-state modifiers the
  // stylesheet already reads off this element.
  const cls = ["msg", p.orgSlot, p.me && "me", p.quoted && "q",
    p.chainStart && "chstart", p.mark === "new" && "isnew"]
    .filter(Boolean)
    .join(" ");
  // The name's hover title, and the avatar's: both name the person the same way,
  // and a caller that supplies no title gets the name it already gave us.
  const who = p.senderTitle ?? p.sender ?? "";
  return (
    <div className={cls} id={p.id} data-ch={p.lane} style={p.style}>
      <div className="col">
        <div className="hdr">
          <Avatar name={p.sender ?? ""} orgSlot={p.orgSlot} pic={p.avatarClass} title={who} />
          <span className="nm" title={who}>
            {p.sender}
          </span>
          <span className="org">{p.org}</span>
          <Stamp id={p.id} stamp={p.stamp} />
          {p.mark === "new" ? <span className="newpill">new</span> : null}
          {p.mark === "revised" ? <span className="revpill">revised</span> : null}
          {p.copyJson !== undefined ? <CopyJson data={p.copyJson} /> : null}
        </div>
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
          <div className="bd" dangerouslySetInnerHTML={html(trimBody(p.body))} />
          {p.edits}
          <Attachments
            attachments={p.attachments}
            extId={p.extId}
            onPull={p.onPull}
            pulling={p.pulling}
            mediaBase={p.mediaBase}
          />
          <div className="foot">
            <span className="to">to {p.to ?? "—"}</span>
            {p.reply}
            {p.source}
          </div>
        </div>
      </div>
    </div>
  );
}
