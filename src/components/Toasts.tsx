import { dismissToast, useToasts } from "../lib/toasts";

/**
 * Where a write says what it did: the bottom right of the window, over
 * everything, whichever page raised it.
 *
 * The corner is the point. These are accounts of work that is over, not state
 * of the mailbox the page keeps reporting, so they must not take a row from the
 * thing the reader is reading — which is exactly what the two earlier homes did:
 * a sentence in the header's own row covered the top of the page, and a sentence
 * under a thread that had just left the list was a claim about mail no longer on
 * screen (the pane is not remounted between threads, so it had to be dropped by
 * hand). Out of the flow, they cover nothing that is being read.
 *
 * It is mounted once, in the shell, so a notification outlives the page that
 * raised it: filing a thread from the pane and then clicking a row in the list
 * does not take away the sentence about what was filed.
 */
export function ToastHost() {
  const toasts = useToasts();
  if (toasts.length === 0) return null;
  return (
    <div className="toasts" role="status" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`toast${t.kind === "fail" ? " bad" : ""}`}>
          <span className="toasttext">{t.text}</span>
          {/* A refusal stands until it is dealt with, so it needs a way out that
              is not "do the thing again". The note has its clock, and this is
              the same affordance either way rather than one shape with a
              deadline and another without. */}
          <button
            type="button"
            className="toastx"
            onClick={() => dismissToast(t.id)}
            aria-label={`Dismiss: ${t.text}`}
            title="Dismiss"
          >
            ×
          </button>
        </div>
      ))}
    </div>
  );
}
