import DialogShell from "../ui/DialogShell";
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
  return (
    <DialogShell
      label="Thread preview"
      onClose={onClose}
      panelClassName="h-[min(82vh,56rem)] max-h-none w-[min(58rem,94vw)] max-w-none overflow-hidden"
    >
      <ThreadPane
        thread={thread}
        label="Thread preview"
        backLabel="Close"
        empty="No entries to show."
        onClose={onClose}
        openInWindow={false}
      />
    </DialogShell>
  );
}
