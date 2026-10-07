import { Button } from "./controls";
export function CompactModeToggle({
  compact,
  onChange,
}: {
  compact: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <Button
      type="button"
      className={`ibcompact-toggle inline-flex items-center gap-[.35rem] whitespace-nowrap rounded-md border border-line bg-transparent px-[.45rem] py-[.25rem] text-[.78rem] text-muted hover:border-accent hover:text-fg ${compact ? "active border-accent bg-mine text-fg" : ""}`}
      title="Show one line per thread"
      aria-pressed={compact}
      onClick={() => onChange(!compact)}
    >
      Compact
    </Button>
  );
}

export function CompactListHeader({ ranked = false }: { ranked?: boolean }) {
  return (
    <div
      className={`ibcompact-head grid grid-cols-[minmax(7rem,1fr)_minmax(0,2fr)_auto_1.5rem] items-center gap-[.8rem] px-[.35rem] py-[.25rem] pl-[.8rem] text-[.68rem] font-semibold text-muted [&>[role=columnheader]:last-child]:text-right ${ranked ? "ranked" : ""}`}
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
