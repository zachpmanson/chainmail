import { EnvelopeIcon } from "@heroicons/react/24/outline";

export default function MailCount({ entries }: { entries: number }) {
  return (
    <span
      className="inline-flex items-center gap-1 text-[.68rem] tabular-nums text-muted"
      title={`${entries} messages in this thread`}
    >
      <EnvelopeIcon width={11} height={11} aria-hidden="true" />
      {entries}
    </span>
  );
}
