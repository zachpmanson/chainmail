import { IconClipboard } from "@tabler/icons-react";
import ReceiptIconButton from "../ui/ReceiptIconButton";
import useOwnToast from "../../lib/ui/useOwnToast";

export default function CopyJson({ data }: { data: unknown }) {
  const say = useOwnToast();
  const label = "Copy this message's JSON";
  return (
    <ReceiptIconButton
      type="button"
      title={label}
      aria-label={label}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(JSON.stringify(data, null, 2));
          say("Copied message JSON");
        } catch {
          say("Could not copy message JSON", "fail");
        }
      }}
    >
      <IconClipboard width={18} height={18} aria-hidden="true" />
    </ReceiptIconButton>
  );
}
