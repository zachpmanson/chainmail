import { useEffect } from "react";
import type { PreviewableThread } from "./ThreadShared";
import ThreadPane from "./ThreadPane";

/** The reading pane as an overlay, for viewports too narrow for a second column. */
export default function ThreadPreview({
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
      className="fixed inset-0 z-[55] flex items-center justify-center bg-black/45"
      role="dialog"
      aria-modal="true"
      aria-label="Thread preview"
      onClick={onClose}
    >
      <div
        className="flex h-[min(82vh,56rem)] w-[min(58rem,94vw)] flex-col overflow-hidden rounded-lg border border-line bg-card shadow-[0_8px_40px_rgba(0,0,0,.35)]"
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
