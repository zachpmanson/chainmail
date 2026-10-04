import type { ReactNode, Ref } from "react";

type Props = {
  variant: "compose" | "reply";
  step: "compose" | "preview" | "done";
  title: string;
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
 *  Variant-specific fields and preview content are supplied by the caller; the
 *  transitions, actions, and safe-to-send review step stay identical. */
export function ComposerFlow({
  variant,
  step,
  title,
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
    <section className={`composer-flow composer-flow--${variant}${variant === "compose" ? " compose-box" : ""}`} aria-labelledby={variant === "compose" ? "composer-title" : undefined}>
      {variant === "compose" ? (
        <header className="compose-head">
          <h2 id="composer-title">{title}</h2>
          {onClose ? <button className="opbtn" type="button" onClick={onClose} aria-label="Close">×</button> : null}
        </header>
      ) : null}
      {step === "done" ? (
        <div role="status">
          {done}
          {onClose ? <button className="opbtn" type="button" onClick={onClose}>{doneLabel}</button> : null}
        </div>
      ) : step === "preview" ? (
        <>
          {preview}
          {error ? <p role="alert">{error}</p> : null}
          <footer className={variant === "reply" ? "opmact replyacts" : undefined}>
            <button className="opbtn" type="button" disabled={busy} onClick={onEdit}>{busy ? "Working…" : editLabel}</button>
            {showConfirm ? <button className={variant === "reply" ? "opbtn opbtn-after" : "opbtn"} type="button" disabled={busy || confirmDisabled} onClick={onConfirm}>{busy ? "Sending…" : confirmLabel}</button> : null}
          </footer>
        </>
      ) : (
        <>
          {error ? <p className={variant === "reply" ? "selfail" : undefined} role="alert">{error}</p> : null}
          {editor}
          <footer className={variant === "reply" ? "opmact replyacts" : undefined}>
            {editorActions}
            {onClose ? <button className="opbtn" type="button" disabled={busy} onClick={onClose}>Cancel</button> : null}
            <button className="opbtn" type="button" title={reviewTitle} disabled={busy || reviewDisabled} onClick={onReview}>{busy ? "Preparing…" : reviewLabel}</button>
          </footer>
        </>
      )}
    </section>
  );

  if (variant === "reply") return <div className="replybox" ref={containerRef}>{content}</div>;
  return <aside className="compose-panel" aria-label="Compose email">{content}</aside>;
}
