import type { ReactNode, Ref } from "react";
import { Button } from "../ui/controls";
import { InlineAlert } from "../ui/InlineAlert";

type Props = {
  variant: "compose" | "reply";
  step: "compose" | "preview" | "done";
  busy: boolean;
  error?: ReactNode;
  editor: ReactNode;
  editorActions?: ReactNode;
  preview: ReactNode;
  done?: ReactNode;
  onReview: () => void;
  onEdit: () => void;
  onConfirm: () => void;
  onClose?: () => void;
  reviewDisabled?: boolean;
  reviewTitle?: string;
  confirmDisabled?: boolean;
  reviewLabel?: string;
  editLabel?: string;
  confirmLabel?: string;
  doneLabel?: string;
  showConfirm?: boolean;
  containerRef?: Ref<HTMLDivElement>;
};

/** Shared compose → preview → confirm shell for new messages and replies.
 *  Variant-specific fields and preview content are supplied by the caller. */
export function ComposerFlow({
  variant,
  step,
  busy,
  error,
  editor,
  editorActions,
  preview,
  done,
  onReview,
  onEdit,
  onConfirm,
  onClose,
  reviewDisabled = false,
  reviewTitle,
  confirmDisabled = false,
  reviewLabel = "Review message",
  editLabel = "Edit",
  confirmLabel = "Confirm and send",
  doneLabel = "Done",
  showConfirm = true,
  containerRef,
}: Props) {
  const content = (
    <section
      className={`composer-flow composer-flow--${variant}`}
      aria-label={variant === "compose" ? "Compose email" : "Reply"}
    >
      {step === "done" ? (
        <div role="status">
          {done}
          {onClose ? (
            <Button
              variant="subtle"
              density="compact"
              className="opbtn"
              type="button"
              onClick={onClose}
            >
              {doneLabel}
            </Button>
          ) : null}
        </div>
      ) : step === "preview" ? (
        <>
          {preview}
          {error ? <InlineAlert>{error}</InlineAlert> : null}
          <footer className="opmact replyacts mt-[.55rem] flex items-center justify-end gap-2">
            <Button
              variant="subtle"
              density="compact"
              className="opbtn"
              type="button"
              disabled={busy}
              onClick={onEdit}
            >
              {busy ? "Working…" : editLabel}
            </Button>
            {showConfirm ? (
              <Button
                variant="danger"
                density="compact"
                className="opbtn opbtn-after"
                type="button"
                disabled={busy || confirmDisabled}
                onClick={onConfirm}
              >
                {busy ? "Sending…" : confirmLabel}
              </Button>
            ) : null}
          </footer>
        </>
      ) : (
        <>
          {error ? <InlineAlert>{error}</InlineAlert> : null}
          {editor}
          <footer className="opmact replyacts mt-[.55rem] flex items-center justify-end gap-2">
            {editorActions}
            {onClose ? (
              <Button
                variant="subtle"
                density="compact"
                className="opbtn"
                type="button"
                disabled={busy}
                onClick={onClose}
              >
                Cancel
              </Button>
            ) : null}
            <Button
              variant="subtle"
              density="compact"
              className="opbtn"
              type="button"
              title={reviewTitle}
              disabled={busy || reviewDisabled}
              onClick={onReview}
            >
              {busy ? "Preparing…" : reviewLabel}
            </Button>
          </footer>
        </>
      )}
    </section>
  );

  const replyBoxClasses =
    "replybox mt-[.9rem] mb-[.3rem] rounded-lg border border-line bg-card px-[.7rem] py-[.6rem]";
  if (variant === "reply")
    return (
      <div className={replyBoxClasses} ref={containerRef}>
        {content}
      </div>
    );
  return (
    <aside
      className="ibread compose-panel flex min-w-0 flex-col overflow-auto border-l border-line bg-bg min-[60rem]:h-full min-[60rem]:min-h-0 min-[60rem]:px-5"
      aria-label="Compose email"
    >
      <div className={replyBoxClasses}>{content}</div>
    </aside>
  );
}
