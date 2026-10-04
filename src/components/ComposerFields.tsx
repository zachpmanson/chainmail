import type { ReactNode } from "react";
import { AddressField, addressKey, addressWords, type Address } from "./AddressField";

type Props = {
  mode: "compose" | "reply";
  to: Address[];
  onToChange: (next: Address[]) => void;
  suggestions: Address[];
  body: string;
  onBodyChange: (next: string) => void;
  busy: boolean;
  subject?: string;
  onSubjectChange?: (next: string) => void;
  cc?: Address[];
  onCcChange?: (next: Address[]) => void;
  editingRecipients?: boolean;
  onEditRecipients?: () => void;
  target?: ReactNode;
  mine?: string[];
};

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

/** The shared message fields used by new mail and replies. The reply supplies its
 * collapsed recipient summary and message target; new mail shows the same address
 * editor expanded and adds a subject field. */
export function ComposerFields({
  mode,
  to,
  onToChange,
  suggestions,
  body,
  onBodyChange,
  busy,
  subject,
  onSubjectChange,
  cc = [],
  onCcChange,
  editingRecipients = true,
  onEditRecipients,
  target,
  mine = [],
}: Props) {
  const hasCc = onCcChange !== undefined;
  const expanded = mode === "compose" || editingRecipients;
  return (
    <>
      <div className="replyrecipients">
        <div className="replyrecipient">
          {expanded ? <>
            <span className="replylabel">{mode === "compose" ? "To" : "to:"}</span>
            <AddressField label="to" value={to} onChange={onToChange} suggestions={suggestions} taken={cc} mine={mine} disabled={busy} />
          </> : (
            <button type="button" className="replysummary" aria-expanded={false} aria-label={`Edit the recipients on this reply — currently ${recipientWords(to, cc)}`} title="Edit the recipients on this reply" disabled={busy} onClick={onEditRecipients}>
              <span className="replylabel">to:</span>
              <span className="replynames">
                {to.length ? names(to) : null}
                {cc.length ? <><span className="replykind">{to.length ? ", cc " : "cc "}</span>{names(cc)}</> : null}
                {!to.length && !cc.length ? <span className="replynone">add an address</span> : null}
              </span>
            </button>
          )}
          {target}
        </div>
        {expanded && hasCc ? <div className="replyrecipient">
          <span className="replylabel">cc:</span>
          <AddressField label="cc" value={cc} onChange={onCcChange} suggestions={suggestions} taken={to} mine={mine} disabled={busy} />
        </div> : null}
      </div>
      {onSubjectChange ? <label>Subject <input className="replyinput" required value={subject ?? ""} disabled={busy} onChange={(e) => onSubjectChange(e.target.value)} /></label> : null}
      <label className={mode === "compose" ? "composer-message" : undefined}>
        {mode === "compose" ? "Message" : null}
        <textarea className="replyinput" aria-label={mode === "reply" ? "Your reply" : undefined} required={mode === "compose"} rows={mode === "compose" ? 10 : 4} value={body} disabled={busy} onChange={(e) => onBodyChange(e.target.value)} />
      </label>
    </>
  );
}
