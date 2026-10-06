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
  from?: ReactNode;
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

/** Shared fields for new mail and replies. Both start with the same collapsed
 * recipient summary and body editor; new mail adds a subject, while replies add
 * Cc and the message target. */
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
  editingRecipients = false,
  onEditRecipients,
  target,
  from,
  mine = [],
}: Props) {
  const hasCc = onCcChange !== undefined;
  const expanded = editingRecipients;
  return (
    <>
      <div className="replyrecipients">
        {expanded ? from : null}
        <div className="replyrecipient">
          {expanded ? <>
            <span className="replylabel">to:</span>
            <AddressField label="to" value={to} onChange={onToChange} suggestions={suggestions} taken={cc} mine={mine} disabled={busy} />
          </> : (
            <button type="button" className="replysummary" aria-expanded={false} aria-label={`Edit recipients — currently ${recipientWords(to, cc)}`} title="Edit recipients" disabled={busy} onClick={onEditRecipients}>
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
      {onSubjectChange ? <label className="replyfield">Subject <input className="replyinput" required value={subject ?? ""} disabled={busy} onChange={(e) => onSubjectChange(e.target.value)} /></label> : null}
      <textarea className="replyinput" aria-label={mode === "reply" ? "Your reply" : "Message"} required={mode === "compose"} rows={4} value={body} disabled={busy} onChange={(e) => onBodyChange(e.target.value)} />
    </>
  );
}
