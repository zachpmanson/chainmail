import { useState } from "react";
import type { Timeline as Spec } from "../lib/spec";
import { Button } from "./controls";

/**
 * Avatars are base64 images that dwarf everything else in the document, so the
 * displayed JSON abbreviates them. Copy still yields the real thing — an
 * abbreviated spec that looks copy-pasteable but isn't would be a trap.
 */
function abbreviate(_key: string, value: unknown) {
  if (typeof value === "string" && value.startsWith("data:") && value.length > 64) {
    const head = value.slice(0, value.indexOf(",") + 1);
    return `${head}… ${(value.length / 1024).toFixed(1)} KB omitted`;
  }
  return value;
}

export function SpecView({ spec, onClose }: { spec: Spec; onClose: () => void }) {
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
      className="specview fixed inset-0 z-[60] flex flex-col bg-bg"
      role="dialog"
      aria-label="Timeline spec as JSON"
    >
      <div className="specbar flex items-center gap-[.6rem] border-b border-line bg-card px-[.8rem] py-2">
        <b className="text-[.72rem] font-bold uppercase tracking-[.09em] text-muted">spec</b>
        <span className="note mr-auto text-[.7rem] text-muted">
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
        className="specpre m-0 flex-1 overflow-auto px-4 pt-[.9rem] pb-8 font-mono text-[.72rem] leading-[1.5] text-fg [tab-size:2] whitespace-pre"
        tabIndex={0}
      >
        {shown}
      </pre>
    </div>
  );
}
