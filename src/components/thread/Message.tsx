import { Fragment, useEffect, useRef, useState } from "react";
import {
  ArrowPathIcon,
  ArrowTopRightOnSquareIcon,
  CheckIcon,
  ClipboardDocumentIcon,
  CodeBracketIcon,
} from "@heroicons/react/24/outline";
import type { CSSProperties, ReactNode } from "react";
import { receiptNames } from "../../lib/message/who";
import Avatar from "./Avatar";
import StatusBadge from "../ui/StatusBadge";
import ReceiptIconButton from "../ui/ReceiptIconButton";
import {
  attHref,
  isSkipped,
  localHref,
  skipNote,
  thumbnail,
  type Attachment,
} from "../../lib/message/attachments";
import type { ZoneState } from "../../lib/timeline/chronological";
import { mountOriginal } from "../../lib/message/original";
import { usePrefs } from "../../lib/prefs/usePrefs";
import { hasBody, trimBody } from "../../lib/message/trimBody";

const html = (s: string) => ({ __html: s });
// The message-level variable colors both the org label and the bubble's decorative stripe.
const orgColors: Record<string, string> = {
  o1: "var(--o1)",
  o2: "var(--o2)",
  o3: "var(--o3)",
  o4: "var(--o4)",
  o5: "var(--o5)",
};

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

function CopyJson({ data }: { data: unknown }) {
  const [done, setDone] = useState(false);
  const label = done ? "Copied" : "Copy this message's JSON";
  return (
    <ReceiptIconButton
      type="button"
      title={label}
      aria-label={label}
      onClick={() => {
        navigator.clipboard?.writeText(JSON.stringify(data, null, 2)).then(
          () => setDone(true),
          () => {},
        );
        window.setTimeout(() => setDone(false), 1200);
      }}
    >
      {done ? (
        <CheckIcon width={18} height={18} aria-hidden="true" />
      ) : (
        <ClipboardDocumentIcon width={18} height={18} aria-hidden="true" />
      )}
    </ReceiptIconButton>
  );
}

function Stamp({ id, stamp }: { id: string; stamp: StampData }) {
  const { date, time, tz, zone } = stamp;
  return (
    <a
      className="whitespace-nowrap text-[.71rem] tabular-nums text-muted"
      href={`#${id}`}
      title="Link to this message"
    >
      {date}
      {time ? ` · ${time}` : ""}
      {zone === "stated" ? <span className="text-[.9em] opacity-[.75]">{tz}</span> : null}
      {zone === "inferred" ? (
        <span
          className="border-b border-dotted border-current text-[.9em] opacity-[.55] [cursor:help]"
          title="Inferred — this source stated no zone. The offset was worked out from the client that quoted this message; see the source notes."
        >{` ${tz}?`}</span>
      ) : null}
      {zone === "unknown" ? (
        <span
          className="text-[.9em] italic tracking-[.02em] opacity-[.45] [cursor:help]"
          title="Zone unknown — this source stated none and nothing available places it. The clock is a wall clock as quoted, so it cannot be compared with the times above and below it."
        >
          {" ?"}
        </span>
      ) : null}
    </a>
  );
}

function Attachments({
  attachments = [],
  extId,
  onPull,
  pulling,
  mediaBase,
}: {
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
  // The endpoint fetches a whole message's files at once, so all its chips share one state.
  const fetching = pulling != null && pulling === extId;
  return (
    <div className="my-1.5 mb-0.5 flex flex-wrap items-center gap-1">
      <span className="text-[.72rem] text-muted" role="img" aria-label="attachments">
        📎
      </span>
      {attachments.map((a, i) => {
        const local = localHref(a, mediaBase ?? "");
        const href = attHref(a, mediaBase);
        // A file the corpus declined can't be requested again, so it stays a plain link.
        const fetchable = !local && !isSkipped(a) && onPull !== undefined && extId !== undefined;
        const shot = thumbnail(a, mediaBase ?? "");
        const thumb = shot ? (
          <img
            className={`block h-[2.1rem] w-auto max-w-36 rounded-[3px] border border-line object-cover object-left${shot.blob ? " w-12" : ""}`}
            src={shot.src}
            {...(shot.w !== undefined ? { width: shot.w } : {})}
            {...(shot.h !== undefined ? { height: shot.h } : {})}
            {...(shot.blob ? { loading: "lazy" as const, decoding: "async" as const } : {})}
            alt=""
          />
        ) : null;
        const label = (
          <>
            {thumb}
            <span className="text-[.74rem] font-[650] font-mono">{a.name}</span>
            <span className={`text-[.64rem] text-muted${fetching && fetchable ? " text-fg" : ""}`}>
              {fetching && fetchable ? (
                /* An image, not a status: the chip has aria-busy and the pane owns the one live region. */
                <ArrowPathIcon
                  className="spinner w-[.838em]"
                  width={16}
                  height={16}
                  role="img"
                  aria-label="Downloading…"
                  aria-hidden={undefined}
                />
              ) : (
                <>
                  {a.kind ?? "file"} · {a.size ?? ""}
                </>
              )}
            </span>
          </>
        );
        // `view` is separate from `open`: a PDF downloads on click but still frames. Markup never
        // gets a view, since a framed sender document would run script in our origin. Pages built
        // before `view` existed lack it, so their PDFs stay plain chips until rebuilt.
        const showsImage = shot !== undefined;
        const view = showsImage ? "image" : local ? (a.view ?? "") : "";
        const opens = view !== "";
        // The popover is layered onto the existing link, so without scripting the click still opens the file.
        const beside = !local || a.open !== "download";
        const note = skipNote(a);
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
            className={[
              "att inline-flex items-baseline gap-1.5 rounded-md border border-line bg-quote px-2 py-0.5 text-fg no-underline hover:border-accent",
              opens && "group/attachment items-center",
              fetching && fetchable && "busy border-accent cursor-progress",
            ]
              .filter(Boolean)
              .join(" ")}
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
            /* The popover's save reads this, not href: Slack hrefs are permalinks and body pictures have none. */
            {...(local ? { "data-get": local } : {})}
            /* Marked on the element, not in state, so the click can be replayed once bytes arrive
               (see behaviour.ts); state wouldn't survive the props change that replaces every
               attachment. Modified clicks fall through to the link. */
            {...(fetchable
              ? {
                  onClick: (ev: React.MouseEvent<HTMLAnchorElement>) => {
                    if (ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.altKey || ev.button !== 0)
                      return;
                    ev.preventDefault();
                    if (fetching) return;
                    ev.currentTarget.setAttribute("data-download", "");
                    onPull!(extId!);
                  },
                }
              : {})}
            {...(fetching && fetchable ? { "aria-busy": true } : {})}
          >
            {label}
            {opens ? (
              <ArrowTopRightOnSquareIcon
                className="size-[.7rem] shrink-0 text-muted group-hover/attachment:text-accent group-focus-visible/attachment:text-accent"
                aria-hidden="true"
              />
            ) : null}
          </a>
        ) : (
          // No popover: it would need a control that does nothing without scripting.
          <span
            key={i}
            className="att inline-flex items-baseline gap-1.5 rounded-md border border-line bg-quote px-2 py-0.5 text-fg no-underline opacity-60 hover:border-accent"
            {...(note ? { title: note } : {})}
          >
            {label}
          </span>
        );
      })}
    </div>
  );
}

/**
 * The sender's own HTML, swapped in for the transcript's rendering. The switch is per
 * sender: stored on the person where there is a corpus, else in the browser's prefs (lib/prefs/usePrefs).
 */
function useOriginal(
  original: { extId: string; load: (extId: string) => Promise<string> } | undefined,
  fromEmail: string | undefined,
  person: { id: number; preferOriginal: boolean } | undefined,
  onPreferOriginal: ((next: boolean) => void) | undefined,
) {
  // A recovered entry has no From header, so the local switch falls back to the message id.
  const key = fromEmail || original?.extId || "";
  const extId = original?.extId;
  const load = original?.load;
  const stored = person !== undefined && onPreferOriginal !== undefined;

  // Locally, one sender's bubbles share the switch, so each follows it rather than owning it.
  const styled = usePrefs((s) => key !== "" && s.styledSenders.includes(key));
  const toggleStyled = usePrefs((s) => s.toggleStyled);
  const on = stored ? person.preferOriginal : styled;
  const [state, setState] = useState<Original>({ at: "read" });
  const arrived = useRef<string | null>(null);

  useEffect(() => {
    if (!extId || !load) return;
    if (!on) {
      // Same object when unchanged, so switching off doesn't re-render every bubble already off.
      setState((s) => (s.at === "read" ? s : { at: "read" }));
      return;
    }
    if (arrived.current !== null) {
      setState({ at: "sent", html: arrived.current });
      return;
    }
    // A fetch landing after the switch went off must not swap the body back.
    let live = true;
    setState({ at: "asking" });
    load(extId).then(
      (html) => {
        arrived.current = html;
        if (live) setState({ at: "sent", html });
      },
      (err: unknown) => {
        if (live) {
          setState({
            at: "none",
            why:
              err instanceof Error && err.message ? err.message : "the original is not available",
          });
        }
      },
    );
    return () => {
      live = false;
    };
  }, [on, extId, load]);

  const ask = () => (stored ? onPreferOriginal(!on) : toggleStyled(key));

  return { on, state, ask };
}

/** The note sits beside the control, not in its place: the switch is the sender's, so it must stay pressable. */
function OriginalControl({ on, state, ask }: { on: boolean; state: Original; ask: () => void }) {
  const asking = state.at === "asking";
  const label =
    state.at === "asking"
      ? "Fetching the sender's own rendering…"
      : on
        ? "Back to the page's own rendering of this sender's mail"
        : "Read this sender's mail as they wrote it, with their own styling";
  return (
    <>
      <ReceiptIconButton
        type="button"
        className={asking ? "origbtn busy cursor-progress" : "origbtn"}
        aria-pressed={on}
        disabled={asking}
        title={label}
        aria-label={label}
        onClick={ask}
      >
        {asking ? (
          <ArrowPathIcon className="spinner" width={18} height={18} aria-hidden="true" />
        ) : (
          <CodeBracketIcon width={18} height={18} aria-hidden="true" />
        )}
      </ReceiptIconButton>
      {state.at === "none" ? (
        <span className="text-[.66rem] italic text-muted" title={state.why}>
          nothing to show
        </span>
      ) : null}
    </>
  );
}

/** Renderings are keyed apart: a shadow root can't be detached, so a reused div would keep drawing the sender's HTML. */
function Body({ body, state }: { body: string; state: Original }) {
  const host = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (state.at === "sent" && host.current) mountOriginal(host.current, state.html);
  }, [state]);

  if (state.at === "sent") {
    return <div key="sent" className="bd bdo overflow-x-auto" ref={host} />;
  }
  if (!hasBody(body)) {
    return (
      <div key="read" className="bd overflow-x-auto">
        <p className="m-0 text-[.8rem] italic text-muted">No body</p>
      </div>
    );
  }
  return (
    <div key="read" className="bd overflow-x-auto" dangerouslySetInnerHTML={html(trimBody(body))} />
  );
}

/** `asking` keeps the transcript's body on screen; `none` is the corpus saying this message has no original part. */
type Original =
  { at: "read" } | { at: "asking" } | { at: "sent"; html: string } | { at: "none"; why: string };

/** One message bubble. */
export default function Message({
  id,
  body,
  sender,
  subject,
  senderTitle,
  org,
  orgSlot,
  avatarClass,
  me,
  quoted,
  landed,
  onLandedEnd,
  mentions,
  fromEmail,
  person,
  onPreferOriginal,
  attachments,
  extId,
  onPull,
  pulling,
  mediaBase,
  to,
  toTitle,
  stamp,
  style,
  lane,
  chainStart,
  mark,
  reply,
  source,
  edits,
  answer,
  copyJson,
  original,
}: {
  /** the bubble's anchor id; the timestamp links to it */
  id: string;
  /** presentation HTML, already sanitised; edges are trimmed here */
  body: string;
  /** the sender as displayed; absent on a message with no name on it */
  sender?: string;
  /** the message's own subject; absent on recovered entries and notes */
  subject?: string;
  /** hover text for the sender, e.g. "Ada Okoye <ada@example.com>"; defaults to the name */
  senderTitle?: string;
  org?: string;
  /** the org's colour slot, e.g. "o2" */
  orgSlot: string;
  /** the sender's avatar image class, e.g. "p0"; absent draws their initials */
  avatarClass?: string;
  /** the reader's own outbound */
  me?: boolean;
  /** reconstructed from quoted text; drawn dashed */
  quoted?: boolean;
  /** the message the pane opened on; flashes once (see .msg.landed) */
  landed?: boolean;
  /** called when that flash finishes, so the caller can take the mark off. */
  onLandedEnd?: () => void;
  /** people @-named in the body, shown above it */
  mentions?: string[];
  /** keys the local styles switch (see lib/prefs/usePrefs); absent on recovered entries */
  fromEmail?: string;
  /** stored half of the styles switch; absent without a corpus (built page, static export) */
  person?: { id: number; preferOriginal: boolean };
  /** flips that preference; absent keeps the switch local */
  onPreferOriginal?: (next: boolean) => void;
  attachments?: Attachment[];
  /** the corpus's handle for this message, which the fetch button asks for */
  extId?: string;
  /** fetch this message's files, where a host will do it at all */
  onPull?: (extId: string) => void;
  /** the message whose files are being fetched, so its button can say so */
  pulling?: string | null;
  /** where the corpus serves stored bytes; empty in the static export */
  mediaBase?: string;
  /** as it appeared on the message, e.g. "Bo Halvorsen, cc …"; absent reads "—" */
  to?: string;
  /** hover text per `to:` name (split by lib/message/who's receiptNames); defaults to the name */
  toTitle?: (name: string) => string;
  stamp: StampData;
  /** where the bubble sits in the transcript grid, from the layout pass */
  style?: CSSProperties;
  /** thread-column index, for the client's column view */
  lane?: number;
  /** opens its thread; marked where the columns are shown */
  chainStart?: boolean;
  /** what changed since a previous render, where there was one */
  mark?: "new" | "revised";
  /** the reply line; a node because only the pipeline can resolve the parent */
  reply?: ReactNode;
  /** where the entry was found, shown in the receipt */
  source?: ReactNode;
  /** a quoter's inline edit to text this message quoted */
  edits?: ReactNode;
  /** control that points the pane's reply box at this message (unlike `reply`, which names its parent) */
  answer?: ReactNode;
  /** what the clip button copies as JSON; absent hides the button */
  copyJson?: unknown;
  /** passed only where the corpus holds the sender's own part and a server can fetch it */
  original?: { extId: string; load: (extId: string) => Promise<string> };
}) {
  const styled = useOriginal(original, fromEmail, person, onPreferOriginal);
  const who = senderTitle ?? sender ?? "";
  return (
    <div
      className={["msg", orgSlot, "mb-2 scroll-mt-6", chainStart && "chstart", landed && "landed"]
        .filter(Boolean)
        .join(" ")}
      id={id}
      data-ch={lane}
      style={
        {
          ...style,
          ...(orgColors[orgSlot] ? { "--orgc": orgColors[orgSlot] } : {}),
        } as CSSProperties
      }
      // Only the flash animation clears the mark; another animation ending must not.
      onAnimationEnd={(e) => {
        if (e.animationName === "flash") onLandedEnd?.();
      }}
    >
      <div className="min-w-0">
        {/* A native <details>, so the export works without scripting and find-in-page reaches the ids. */}
        <details className="hdr mb-0.5 px-0.5">
          <summary className="flex cursor-pointer flex-wrap items-baseline gap-1 list-none focus-visible:outline focus-visible:outline-1 focus-visible:outline-accent focus-visible:outline-offset-2">
            <Avatar name={sender ?? ""} orgSlot={orgSlot} pic={avatarClass} title={who} />
            <span className="text-[.83rem] font-[650]" title={who}>
              {sender}
            </span>
            <span className="text-[var(--orgc,var(--muted))] text-[.68rem] font-[650] uppercase tracking-[.07em]">
              {org}
            </span>
            <Stamp id={id} stamp={stamp} />
            {mark === "new" ? <StatusBadge tone="new">new</StatusBadge> : null}
            {mark === "revised" ? <StatusBadge tone="revised">revised</StatusBadge> : null}
            {/* Always drawn: the caret lives in this box. */}
            <span className="htail ml-auto inline-flex items-baseline gap-2">{reply}</span>
          </summary>
          <div className="mb-1 flex flex-wrap items-center gap-x-3 gap-y-1 px-0.5 pt-1">
            {subject || source ? (
              <span className="flex flex-[1_1_100%] items-baseline gap-x-3 gap-y-1">
                {subject ? (
                  <span
                    className="min-w-0 flex-[1_1_auto] text-[.72rem] leading-[1.25] text-fg"
                    title={subject}
                  >
                    {subject}
                  </span>
                ) : null}
                {source}
              </span>
            ) : null}
            <span className="text-[.66rem] text-muted">
              to{" "}
              {to
                ? receiptNames(to).map((r, i) => (
                    <Fragment key={`${r.name}-${i}`}>
                      {i === 0 ? "" : ", "}
                      <span title={toTitle ? toTitle(r.name) : r.name}>{r.text}</span>
                    </Fragment>
                  ))
                : "—"}
            </span>
            {answer !== undefined || original !== undefined || copyJson !== undefined ? (
              <span className="ml-auto inline-flex items-center gap-1.5">
                {answer}
                {original !== undefined ? (
                  <OriginalControl on={styled.on} state={styled.state} ask={styled.ask} />
                ) : null}
                {copyJson !== undefined ? <CopyJson data={copyJson} /> : null}
              </span>
            ) : null}
          </div>
        </details>
        <div
          className={[
            // The stripe is a pseudo-element clipped by overflow-hidden, independent of border/background states.
            "bub relative overflow-hidden rounded-[10px] border border-line bg-card px-3 py-2 before:absolute before:inset-y-0 before:left-0 before:w-[3px] before:bg-[var(--orgc,transparent)] before:content-['']",
            quoted && "border-dashed border-muted/55 bg-dash",
            me && "border-org-3 bg-mine",
            mark === "new" && "border-l-[3px] border-l-org-1",
          ]
            .filter(Boolean)
            .join(" ")}
        >
          {mentions?.length ? (
            <div className="mb-1 flex flex-wrap gap-1">
              {mentions.map((m) => (
                <span
                  className="rounded-[5px] bg-mine px-1.5 text-[.74rem] font-semibold text-org-3"
                  key={m}
                >
                  @{m}
                </span>
              ))}
            </div>
          ) : null}
          <Body body={body} state={styled.state} />
          {edits}
          <Attachments
            attachments={attachments}
            extId={extId}
            onPull={onPull}
            pulling={pulling}
            mediaBase={mediaBase}
          />
        </div>
      </div>
    </div>
  );
}
