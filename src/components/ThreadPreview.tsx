import { useEffect } from "react";
import type { PreviewableThread } from "./ThreadShared";
import { ThreadPane } from "./ThreadPane";

export { Failure, statusLabel } from "./ThreadShared";
export type { PreviewableThread } from "./ThreadShared";

/** The same full reader used beside a list, presented over it when the viewport
 *  has no room for a second column. Only the container and dismissal differ. */
export function ThreadPreview({ thread, onClose }: { thread: PreviewableThread; onClose: () => void }) {
  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="selpv selpv-thread" role="dialog" aria-modal="true" aria-label="Thread preview" onClick={onClose}>
      <div className="selpv-panel" onClick={(e) => e.stopPropagation()}>
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
