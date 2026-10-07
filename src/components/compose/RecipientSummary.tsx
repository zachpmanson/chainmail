import type { ReactNode } from "react";
import { addressKey, addressWords, type Address } from "./AddressField";

function names(list: Address[]) {
  return list.map((a, i) => (
    <span key={addressKey(a.address)} title={addressWords(a)}>
      {i === 0 ? null : ", "}
      {a.name || a.address}
    </span>
  ));
}

function recipientWords(to: Address[], cc: Address[]) {
  const label = (list: Address[]) => list.map((a) => a.name || a.address).join(", ");
  const parts: string[] = [];
  if (to.length) parts.push(`to ${label(to)}`);
  if (cc.length) parts.push(`cc ${label(cc)}`);
  return parts.join(", ") || "nobody";
}

/** The collapsed "to …" line, styled like Message's header. */
export default function RecipientSummary({
  to,
  cc,
  busy,
  onEdit,
  target,
}: {
  to: Address[];
  cc: Address[];
  busy: boolean;
  onEdit: () => void;
  target: ReactNode;
}) {
  return (
    <span className="flex min-w-0 items-center gap-1">
      <button
        type="button"
        className="flex h-7 min-w-0 flex-1 cursor-pointer items-center overflow-hidden border-0 bg-transparent p-0 text-left font-[inherit] text-2xs text-muted disabled:cursor-default disabled:opacity-55 focus-visible:rounded-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        aria-expanded={false}
        aria-label={`Edit recipients — currently ${recipientWords(to, cc)}`}
        title="Edit recipients"
        disabled={busy}
        onClick={onEdit}
      >
        <span className="min-w-0 truncate">
          to {to.length ? names(to) : null}
          {cc.length ? (
            <>
              <span>{to.length ? ", cc " : "cc "}</span>
              {names(cc)}
            </>
          ) : null}
          {!to.length && !cc.length ? <span className="italic">add an address</span> : null}
        </span>
      </button>
      {target}
    </span>
  );
}
