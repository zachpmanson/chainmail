import { Button } from "../ui/controls";

export default function CompactModeToggle({
  compact,
  onChange,
}: {
  compact: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <Button
      type="button"
      className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-md border border-line bg-transparent px-2 py-1 text-[.78rem] text-muted hover:border-accent hover:text-fg ${compact ? "border-accent bg-mine text-fg" : ""}`}
      title="Show one line per thread"
      aria-pressed={compact}
      onClick={() => onChange(!compact)}
    >
      Compact
    </Button>
  );
}
