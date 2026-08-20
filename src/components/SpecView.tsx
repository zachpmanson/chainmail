import { useState } from "react";
import type { Timeline as Spec } from "../lib/spec";

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
    <div className="specview" role="dialog" aria-label="Timeline spec as JSON">
      <div className="specbar">
        <b>spec</b>
        <span className="note">
          {spec.messages.length - notes} messages · {notes} notices ·{" "}
          {(bytes / 1024).toFixed(0)} KB · images abbreviated for display
        </span>
        <button className="tbtn" type="button" onClick={copy}>
          {copied ? "copied" : "copy"}
        </button>
        <button className="tbtn" type="button" onClick={onClose}>
          close
        </button>
      </div>
      <pre className="specpre" tabIndex={0}>
        {shown}
      </pre>
    </div>
  );
}
