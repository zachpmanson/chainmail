import { useEffect } from "react";
import type { PreviewableThread } from "./ThreadShared";
import { ThreadPane } from "./ThreadPane";

export { Failure, statusLabel } from "./ThreadShared";
export type { PreviewableThread } from "./ThreadShared";

/** The same full reader used beside a list, presented over it when the viewport
 *  has no room for a second column. Only the container and dismissal differ. */
export function ThreadPreview({
  thread,
  onClose,
}: {
  thread: PreviewableThread;
  onClose: () => void;
}) {
  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="selpv selpv-thread fixed inset-0 z-[55] flex items-center justify-center bg-black/45"
      role="dialog"
      aria-modal="true"
      aria-label="Thread preview"
      onClick={onClose}
    >
      <div
        className="selpv-panel flex h-[min(82vh,56rem)] w-[min(58rem,94vw)] flex-col overflow-hidden rounded-lg border border-line bg-card shadow-[0_8px_40px_rgba(0,0,0,.35)]"
        onClick={(e) => e.stopPropagation()}
      >
        <ThreadPane
          thread={thread}
          label="Thread preview"
          backLabel="Close"
          empty="No entries to show."
          onClose={onClose}
          openInWindow={false}
        />
      </div>
    </div>
  );
}
