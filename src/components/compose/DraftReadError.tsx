import Button from "../ui/Button";
import InlineAlert from "../ui/InlineAlert";
import { errText } from "../../lib/ui/errText";

export default function DraftReadError({
  error,
  retrying,
  onRetry,
}: {
  error: unknown;
  retrying: boolean;
  onRetry: () => void;
}) {
  return (
    <InlineAlert>
      Gmail drafts could not be read: {errText(error)}. Autosave is paused. Copy your text before
      leaving.
      <Button
        type="button"
        variant="subtle"
        density="compact"
        className="ml-2"
        disabled={retrying}
        onClick={onRetry}
      >
        {retrying ? "Checking…" : "Retry draft check"}
      </Button>
    </InlineAlert>
  );
}
