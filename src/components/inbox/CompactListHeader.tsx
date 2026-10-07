export default function CompactListHeader() {
  return (
    <div
      className="grid grid-cols-[minmax(7rem,1fr)_minmax(0,2fr)_auto_1.5rem] items-center gap-3 px-1.5 py-1 pl-3 text-[.68rem] font-semibold text-muted [&>[role=columnheader]:last-child]:text-right"
      role="row"
      aria-hidden="true"
    >
      <span role="columnheader">Sender</span>
      <span role="columnheader">Subject</span>
      <span role="columnheader">Time</span>
      <span role="columnheader" aria-label="Select"></span>
    </div>
  );
}
