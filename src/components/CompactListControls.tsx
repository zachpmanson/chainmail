export function CompactModeToggle({ compact, onChange }: { compact: boolean; onChange: (value: boolean) => void }) {
  return (
    <button
      type="button"
      className={`ibcompact-toggle${compact ? " active" : ""}`}
      title="Show one line per thread"
      aria-pressed={compact}
      onClick={() => onChange(!compact)}
    >
      Compact
    </button>
  );
}

export function CompactListHeader({ ranked = false }: { ranked?: boolean }) {
  return (
    <div className={`ibcompact-head${ranked ? " ranked" : ""}`} role="row" aria-hidden="true">
      <span role="columnheader">Sender</span>
      <span role="columnheader">Subject</span>
      <span role="columnheader">Time</span>
      <span role="columnheader" aria-label="Select"></span>
    </div>
  );
}
