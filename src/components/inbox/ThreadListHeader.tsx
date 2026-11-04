import { useLayoutEffect, useRef } from "react";
import Checkbox from "../ui/Checkbox";

export default function ThreadListHeader({
  compact,
  checked,
  mixed,
  onChange,
}: {
  compact: boolean;
  checked: boolean;
  mixed: boolean;
  onChange: (checked: boolean) => void;
}) {
  const input = useRef<HTMLInputElement>(null);
  useLayoutEffect(() => {
    if (input.current) input.current.indeterminate = mixed;
  }, [mixed, checked]);
  return (
    <div className="relative flex min-h-8 items-center gap-3 py-1 pr-9 pl-3 text-2xs font-semibold text-muted">
      {compact ? (
        <>
          <span className="min-w-28 flex-1" aria-hidden="true">
            Sender
          </span>
          <span className="min-w-0 flex-2" aria-hidden="true">
            Subject
          </span>
          <span className="shrink-0" aria-hidden="true">
            Time
          </span>
        </>
      ) : (
        <span className="flex-1">Shown threads</span>
      )}
      <label
        className="absolute top-1/2 right-0 flex size-8 -translate-y-1/2 cursor-pointer items-center justify-center select-none"
        title="Select or deselect only threads already loaded in this list; this does not load more or select the whole mailbox."
      >
        <Checkbox
          ref={input}
          checked={checked}
          aria-label="Select all shown threads"
          className="m-0 size-6 cursor-pointer"
          onChange={(event) => onChange(event.currentTarget.checked)}
        />
      </label>
    </div>
  );
}
