import { useState } from "react";
import { CheckIcon, ClipboardDocumentIcon } from "@heroicons/react/24/outline";
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
        <CheckIcon width={18} height={18} aria-hidden="true" />
      ) : (
        <ClipboardDocumentIcon width={18} height={18} aria-hidden="true" />
      )}
    </ReceiptIconButton>
  );
}
