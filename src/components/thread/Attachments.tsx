import type { Attachment } from "../../lib/message/attachments";
import AttachmentChip from "./AttachmentChip";

export default function Attachments({
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
      <span className="text-xs text-muted" role="img" aria-label="attachments">
        📎
      </span>
      {attachments.map((a, i) => (
        <AttachmentChip
          key={i}
          attachment={a}
          mediaBase={mediaBase}
          fetching={fetching}
          onPull={onPull && extId !== undefined ? () => onPull(extId) : undefined}
        />
      ))}
    </div>
  );
}
