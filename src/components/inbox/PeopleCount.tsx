import { IconUsers } from "@tabler/icons-react";

/** Zero is printed: a thread of recovered quotes has no address to count. */
export default function PeopleCount({ people }: { people: number }) {
  return (
    <span
      className="inline-flex items-center gap-1 text-2xs text-muted tabular-nums"
      title={`${people} people in this thread — senders and recipients`}
    >
      <IconUsers width={11} height={11} aria-hidden="true" />
      {people}
    </span>
  );
}
