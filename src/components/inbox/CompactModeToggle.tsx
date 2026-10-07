import Button from "../ui/Button";

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
      className={`inline-flex items-center gap-1.5 rounded-md border border-line bg-transparent px-2 py-1 text-xs whitespace-nowrap text-muted hover:border-accent hover:text-fg ${compact ? "border-accent bg-mine text-fg" : ""}`}
      title="Show one line per thread"
      aria-pressed={compact}
      onClick={() => onChange(!compact)}
    >
      Compact
    </Button>
  );
}
