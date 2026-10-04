import type { ReactNode, Ref } from "react";

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
    <section className={`composer-flow composer-flow--${variant}`} aria-label={variant === "compose" ? "Compose email" : "Reply"}>
      {step === "done" ? (
        <div role="status">
          {done}
          {onClose ? <button className="opbtn" type="button" onClick={onClose}>{doneLabel}</button> : null}
        </div>
      ) : step === "preview" ? (
        <>
          {preview}
          {error ? <p className="selfail" role="alert">{error}</p> : null}
          <footer className="opmact replyacts">
            <button className="opbtn" type="button" disabled={busy} onClick={onEdit}>{busy ? "Working…" : editLabel}</button>
            {showConfirm ? <button className="opbtn opbtn-after" type="button" disabled={busy || confirmDisabled} onClick={onConfirm}>{busy ? "Sending…" : confirmLabel}</button> : null}
          </footer>
        </>
      ) : (
        <>
          {error ? <p className="selfail" role="alert">{error}</p> : null}
          {editor}
          <footer className="opmact replyacts">
            {editorActions}
            {onClose ? <button className="opbtn" type="button" disabled={busy} onClick={onClose}>Cancel</button> : null}
            <button className="opbtn" type="button" title={reviewTitle} disabled={busy || reviewDisabled} onClick={onReview}>{busy ? "Preparing…" : reviewLabel}</button>
          </footer>
        </>
      )}
    </section>
  );

  if (variant === "reply") return <div className="replybox" ref={containerRef}>{content}</div>;
  return <aside className="ibread compose-panel" aria-label="Compose email"><div className="replybox">{content}</div></aside>;
}
