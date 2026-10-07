import { ArrowPathIcon, CodeBracketIcon } from "@heroicons/react/24/outline";
import type { Original } from "../../lib/message/useOriginal";
import ReceiptIconButton from "../ui/ReceiptIconButton";

/** The note sits beside the control, not in its place: the switch is the sender's, so it must stay pressable. */
export default function OriginalControl({
  on,
  state,
  ask,
}: {
  on: boolean;
  state: Original;
  ask: () => void;
}) {
  const asking = state.at === "asking";
  const label =
    state.at === "asking"
      ? "Fetching the sender's own rendering…"
      : on
        ? "Back to the page's own rendering of this sender's mail"
        : "Read this sender's mail as they wrote it, with their own styling";
  return (
    <>
      <ReceiptIconButton
        type="button"
        className={asking ? "cursor-progress" : undefined}
        aria-pressed={on}
        disabled={asking}
        title={label}
        aria-label={label}
        onClick={ask}
      >
        {asking ? (
          <ArrowPathIcon className="motion-safe:animate-spin" aria-hidden="true" />
        ) : (
          <CodeBracketIcon width={18} height={18} aria-hidden="true" />
        )}
      </ReceiptIconButton>
      {state.at === "none" ? (
        <span className="text-2xs text-muted italic" title={state.why}>
          nothing to show
        </span>
      ) : null}
    </>
  );
}
