import { useState } from "react";
import { IconCheck, IconClipboard } from "@tabler/icons-react";
import ReceiptIconButton from "../ui/ReceiptIconButton";

export default function CopyJson({ data }: { data: unknown }) {
  const [done, setDone] = useState(false);
  const label = done ? "Copied" : "Copy this message's JSON";
  return (
    <ReceiptIconButton
      type="button"
      title={label}
      aria-label={label}
      onClick={() => {
        navigator.clipboard?.writeText(JSON.stringify(data, null, 2)).then(
          () => setDone(true),
          () => {},
        );
        window.setTimeout(() => setDone(false), 1200);
      }}
    >
      {done ? (
        <IconCheck width={18} height={18} aria-hidden="true" />
      ) : (
        <IconClipboard width={18} height={18} aria-hidden="true" />
      )}
    </ReceiptIconButton>
  );
}
