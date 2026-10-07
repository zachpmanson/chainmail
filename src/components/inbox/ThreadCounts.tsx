import AttachmentCount from "./AttachmentCount";
import MailCount from "./MailCount";
import PeopleCount from "./PeopleCount";

/** The counts shown at the end of a thread row, with the row's minimum-noise thresholds. */
export default function ThreadCounts({
  people,
  entries,
  attachments,
  className = "",
}: {
  people: number;
  entries: number;
  attachments: number;
  className?: string;
}) {
  return (
    <span className={`ml-auto flex shrink-0 items-center gap-2 ${className}`.trim()}>
      {people > 2 ? <PeopleCount people={people} /> : null}
      {entries > 1 ? <MailCount entries={entries} /> : null}
      {attachments > 0 ? <AttachmentCount attachments={attachments} /> : null}
    </span>
  );
}
