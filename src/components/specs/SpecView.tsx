import { useState } from "react";
import type { Timeline as Spec } from "../../lib/timeline/spec";
import { Button } from "../ui/controls";

/** Abbreviates base64 avatars for display only; Copy still yields the full spec. */
function abbreviate(_key: string, value: unknown) {
  if (typeof value === "string" && value.startsWith("data:") && value.length > 64) {
    const head = value.slice(0, value.indexOf(",") + 1);
    return `${head}… ${(value.length / 1024).toFixed(1)} KB omitted`;
  }
  return value;
}

export default function SpecView({ spec, onClose }: { spec: Spec; onClose: () => void }) {
  const [copied, setCopied] = useState(false);
  const shown = JSON.stringify(spec, abbreviate, 2);
  const full = JSON.stringify(spec, null, 2);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(full);
      setCopied(true);
      setTimeout(() => setCopied(false), 1400);
    } catch {
      setCopied(false);
    }
  };

  const bytes = new Blob([full]).size;
  const notes = spec.messages.filter((m) => m.kind === "note").length;

  return (
    <div
      className="fixed inset-0 z-60 flex flex-col bg-bg"
      role="dialog"
      aria-label="Timeline spec as JSON"
    >
      <div className="flex items-center gap-2 border-b border-line bg-card px-3 py-2">
        <b className="text-xs font-bold uppercase tracking-[.09em] text-muted">spec</b>
        <span className="mr-auto text-xs text-muted">
          {spec.messages.length - notes} messages · {notes} notices · {(bytes / 1024).toFixed(0)} KB
          · images abbreviated for display
        </span>
        <Button variant="secondary" density="compact" type="button" onClick={copy}>
          {copied ? "copied" : "copy"}
        </Button>
        <Button variant="secondary" density="compact" type="button" onClick={onClose}>
          close
        </Button>
      </div>
      <pre
        className="m-0 flex-1 overflow-auto px-4 pt-4 pb-8 font-mono text-xs leading-normal text-fg tab-2 whitespace-pre"
        tabIndex={0}
      >
        {shown}
      </pre>
    </div>
  );
}
