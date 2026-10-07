import { PaperClipIcon } from "@heroicons/react/24/outline";

export default function AttachmentCount({ attachments }: { attachments: number }) {
  return (
    <span
      className="inline-flex items-center gap-1 text-2xs tabular-nums text-muted"
      title={`${attachments} attachment${attachments === 1 ? "" : "s"} in this thread`}
    >
      <PaperClipIcon width={11} height={11} aria-hidden="true" />
      {attachments}
    </span>
  );
}
