import { ArrowPathIcon, ArrowTopRightOnSquareIcon } from "@heroicons/react/24/outline";
import {
  attHref,
  isSkipped,
  localHref,
  skipNote,
  thumbnail,
  type Attachment,
} from "../../lib/message/attachments";

/** One file under a message. `data-attachment` is what client/behaviour.ts and
 *  lib/thread/viewTransition.ts find chips by. */
export default function AttachmentChip({
  attachment: a,
  mediaBase,
  fetching,
  onPull,
}: {
  attachment: Attachment;
  /** where the corpus serves stored bytes; empty in the static export */
  mediaBase?: string;
  /** this message's files are being fetched */
  fetching: boolean;
  /** fetch this message's files, where a host will do it at all */
  onPull?: () => void;
}) {
  const local = localHref(a, mediaBase ?? "");
  const href = attHref(a, mediaBase);
  // A file the corpus declined can't be requested again, so it stays a plain link.
  const fetchable = !local && !isSkipped(a) && onPull !== undefined;
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
            className="inline-block size-[1em] align-middle motion-safe:animate-spin"
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
      className={[
        "inline-flex items-baseline gap-1.5 rounded-md border border-line bg-quote px-2 py-0.5 text-fg no-underline hover:border-accent",
        opens && "group/attachment items-center",
        fetching && fetchable && "border-accent cursor-progress",
      ]
        .filter(Boolean)
        .join(" ")}
      data-attachment
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
         (see client/behaviour.ts); state wouldn't survive the props change that replaces every
         attachment. Modified clicks fall through to the link. */
      {...(fetchable
        ? {
            onClick: (ev: React.MouseEvent<HTMLAnchorElement>) => {
              if (ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.altKey || ev.button !== 0) return;
              ev.preventDefault();
              if (fetching) return;
              ev.currentTarget.setAttribute("data-download", "");
              onPull!();
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
      className="inline-flex items-baseline gap-1.5 rounded-md border border-line bg-quote px-2 py-0.5 text-fg no-underline opacity-60 hover:border-accent"
      data-attachment
      {...(note ? { title: note } : {})}
    >
      {label}
    </span>
  );
}
