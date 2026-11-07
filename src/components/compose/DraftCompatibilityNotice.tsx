import type useDraftPreflight from "./useDraftPreflight";
import type { RecoveryDraft } from "../../lib/ui/draftRecovery";
import DraftSaveError from "./DraftSaveError";
import Button from "../ui/Button";

export default function DraftCompatibilityNotice({
  check,
  draft,
}: {
  check: ReturnType<typeof useDraftPreflight>;
  draft: RecoveryDraft;
}) {
  if (!check.message) return null;
  return (
    <DraftSaveError error={check.message} draft={draft}>
      {check.canRetry ? (
        <Button type="button" density="compact" disabled={check.retrying} onClick={check.retry}>
          Retry compatibility check
        </Button>
      ) : null}
    </DraftSaveError>
  );
}
