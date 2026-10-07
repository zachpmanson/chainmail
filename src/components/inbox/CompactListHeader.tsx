export default function CompactListHeader() {
  return (
    <div
      className="flex items-center gap-3 px-1.5 py-1 pl-3 text-2xs font-semibold text-muted"
      role="row"
      aria-hidden="true"
    >
      <span className="min-w-28 flex-1" role="columnheader">
        Sender
      </span>
      <span className="min-w-0 flex-2" role="columnheader">
        Subject
      </span>
      <span className="shrink-0" role="columnheader">
        Time
      </span>
      <span className="w-6 shrink-0" role="columnheader" aria-label="Select"></span>
    </div>
  );
}
