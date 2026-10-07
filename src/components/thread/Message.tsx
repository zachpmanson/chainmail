import { Fragment } from "react";
import type { CSSProperties, ReactNode } from "react";
import { receiptNames } from "../../lib/message/who";
import type { MessageEmail } from "../../lib/message/email";
import { useOriginal } from "../../lib/message/useOriginal";
import StatusBadge from "../ui/StatusBadge";
import Attachments from "./Attachments";
import Avatar from "./Avatar";
import Body from "./Body";
import CopyJson from "./CopyJson";
import OriginalControl from "./OriginalControl";
import Stamp from "./Stamp";

// The message-level variable colors both the org label and the bubble's decorative stripe.
const orgColors: Record<string, string> = {
  o1: "var(--o1)",
  o2: "var(--o2)",
  o3: "var(--o3)",
  o4: "var(--o4)",
  o5: "var(--o5)",
};

/** Where the bubble sits in its view, and its transient marks. */
export type MessagePlace = {
  /** the bubble's anchor id; the timestamp links to it */
  id: string;
  /** where the bubble sits in the transcript grid, from the layout pass */
  style?: CSSProperties;
  /** thread-column index, for the client's column view */
  lane?: number;
  /** opens its thread; marked where the columns are shown */
  chainStart?: boolean;
  /** what changed since a previous render, where there was one */
  mark?: "new" | "revised";
  /** the message the pane opened on; flashes once */
  landed?: boolean;
  /** called when that flash finishes, so the caller can take the mark off. */
  onLandedEnd?: () => void;
};

/** Presentation lookups. */
export type MessageLook = {
  /** the org's colour slot, e.g. "o2" */
  orgSlot: string;
  /** the sender's avatar image class, e.g. "p0"; absent draws their initials */
  avatarClass?: string;
  /** hover text per `to:` name (split by lib/message/who's receiptNames); defaults to the name */
  toTitle?: (name: string) => string;
};

/** Fetching a message's files; absent where no host will do it. */
export type MessageMedia = {
  onPull: (extId: string) => void;
  /** the message whose files are being fetched, so its button can say so */
  pulling: string | null;
  /** where the corpus serves stored bytes */
  mediaBase: string;
};

/** The original-styling switch; absent unless the corpus holds the sender's own part and a server can fetch it. */
export type MessageReading = {
  original: { extId: string; load: (extId: string) => Promise<string> };
  /** stored half of the switch; absent keeps it in browser storage */
  person?: { id: number; preferOriginal: boolean };
  /** flips that preference; absent keeps the switch local */
  onPreferOriginal?: (next: boolean) => void;
};

/** One message bubble. */
export default function Message({
  email: {
    sender,
    senderTitle,
    fromEmail,
    org,
    to,
    subject,
    body,
    stamp,
    attachments,
    mentions,
    extId,
    me,
    quoted,
  },
  place: { id, style, lane, chainStart, mark, landed, onLandedEnd },
  look: { orgSlot, avatarClass, toTitle },
  media,
  reading,
  reply,
  source,
  edits,
  answer,
  copyJson,
}: {
  email: MessageEmail;
  place: MessagePlace;
  look: MessageLook;
  media?: MessageMedia;
  reading?: MessageReading;
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
}) {
  const styled = useOriginal(
    reading?.original,
    fromEmail,
    reading?.person,
    reading?.onPreferOriginal,
  );
  const who = senderTitle ?? sender ?? "";
  return (
    <div
      className={["msg", "mb-2 scroll-mt-6", chainStart && "chstart"].filter(Boolean).join(" ")}
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
          <summary className="flex cursor-pointer flex-wrap items-baseline gap-1 list-none focus-visible:outline focus-visible:outline-accent focus-visible:outline-offset-2">
            <Avatar name={sender ?? ""} orgSlot={orgSlot} pic={avatarClass} title={who} />
            <span className="text-sm font-[650]" title={who}>
              {sender}
            </span>
            <span className="text-(--orgc,var(--muted)) text-2xs font-[650] uppercase tracking-[.07em]">
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
                  <span className="min-w-0 flex-[1_1_auto] text-xs/tight text-fg" title={subject}>
                    {subject}
                  </span>
                ) : null}
                {source}
              </span>
            ) : null}
            <span className="text-2xs text-muted">
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
            {answer !== undefined || reading !== undefined || copyJson !== undefined ? (
              <span className="ml-auto inline-flex items-center gap-1.5">
                {answer}
                {reading !== undefined ? (
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
            "bub relative overflow-hidden rounded-lg border border-line bg-card px-3 py-2 before:absolute before:inset-y-0 before:left-0 before:w-[3px] before:bg-(--orgc,transparent) before:content-['']",
            quoted && "border-dashed border-muted/55 bg-dash",
            me && "border-org-3 bg-mine",
            mark === "new" && "border-l-3 border-l-org-1",
            landed
              ? "animate-[flash_1.6s_ease-out_1] border-accent"
              : "in-target:animate-[flash_1.4s_ease-out_1] in-target:border-accent",
          ]
            .filter(Boolean)
            .join(" ")}
        >
          {mentions?.length ? (
            <div className="mb-1 flex flex-wrap gap-1">
              {mentions.map((m) => (
                <span
                  className="rounded-md bg-mine px-1.5 text-xs font-semibold text-org-3"
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
            onPull={media?.onPull}
            pulling={media?.pulling}
            mediaBase={media?.mediaBase}
          />
        </div>
      </div>
    </div>
  );
}
