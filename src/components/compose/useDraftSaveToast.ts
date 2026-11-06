import { useEffect, useRef } from "react";
import { dismissToast, pushToast, SAID_MS } from "../../lib/ui/toasts";

/** Only autosave completions (not hydrated acknowledgements) can raise a saved note. */
export default function useDraftSaveToast({
  completion,
  currentAcknowledgement,
  saved,
  context,
}: {
  completion: { acknowledgement: string; context: string } | null;
  currentAcknowledgement: string;
  saved: boolean;
  context: string;
}) {
  const toast = useRef<number | null>(null);
  // Object identity records every completion, including A → stale B → A saves.
  const handled = useRef(completion);
  useEffect(() => {
    const dismiss = () => {
      if (toast.current !== null) dismissToast(toast.current);
      toast.current = null;
    };
    const current =
      completion?.acknowledgement === currentAcknowledgement && completion.context === context;
    if (!saved || !current) {
      dismiss();
      // Never resurrect an older completion if the editor later returns to its words.
      if (!current) handled.current = completion;
      return;
    }
    if (completion === handled.current) return;
    handled.current = completion;
    dismiss();
    toast.current = pushToast("Saved to Gmail", "note", SAID_MS);
  }, [completion, currentAcknowledgement, saved, context]);
  useEffect(
    () => () => {
      if (toast.current !== null) dismissToast(toast.current);
    },
    [],
  );
}
