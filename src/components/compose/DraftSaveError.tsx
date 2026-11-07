import type { ReactNode } from "react";
import { draftRecoveryText, type RecoveryDraft } from "../../lib/ui/draftRecovery";
import useOwnToast from "../../lib/ui/useOwnToast";
import Button from "../ui/Button";
import InlineAlert from "../ui/InlineAlert";

export default function DraftSaveError({
  error,
  draft,
  children,
}: {
  error: string;
  draft: RecoveryDraft;
  children?: ReactNode;
}) {
  const say = useOwnToast();
  return (
    <InlineAlert>
      {error}{" "}
      <Button
        type="button"
        density="compact"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(draftRecoveryText(draft));
            say("Copied draft text");
          } catch {
            say("Could not copy draft text", "fail");
          }
        }}
      >
        Copy draft text
      </Button>
      {children}
    </InlineAlert>
  );
}
