import type { ReactNode } from "react";
import { FormField } from "../ui/FormField";
import { Button, TextArea, TextInput } from "../ui/controls";
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
      <div className="replyrecipients [--addrrow:1.7rem] grid grid-cols-[max-content_minmax(0,1fr)_max-content] items-start gap-x-[.3rem] gap-y-[.4rem] mb-[.55rem] text-[.74rem] text-muted">
        {expanded ? from : null}
        <div className="replyrecipient contents min-w-0">
          {expanded ? (
            <>
              <span className="replylabel inline-flex h-[var(--addrrow)] items-center">to:</span>
              <AddressField
                label="to"
                value={to}
                onChange={onToChange}
                suggestions={suggestions}
                taken={cc}
                mine={mine}
                disabled={busy}
              />
            </>
          ) : (
            <Button
              variant="quiet"
              type="button"
              className="replysummary"
              aria-expanded={false}
              aria-label={`Edit recipients — currently ${recipientWords(to, cc)}`}
              title="Edit recipients"
              disabled={busy}
              onClick={onEditRecipients}
            >
              <span className="replylabel inline-flex h-[var(--addrrow)] items-center">to:</span>
              <span className="replynames">
                {to.length ? names(to) : null}
                {cc.length ? (
                  <>
                    <span className="replykind">{to.length ? ", cc " : "cc "}</span>
                    {names(cc)}
                  </>
                ) : null}
                {!to.length && !cc.length ? (
                  <span className="replynone">add an address</span>
                ) : null}
              </span>
            </Button>
          )}
          {target}
        </div>
        {expanded && hasCc ? (
          <div className="replyrecipient contents min-w-0">
            <span className="replylabel inline-flex h-[var(--addrrow)] items-center">cc:</span>
            <AddressField
              label="cc"
              value={cc}
              onChange={onCcChange}
              suggestions={suggestions}
              taken={to}
              mine={mine}
              disabled={busy}
            />
          </div>
        ) : null}
      </div>
      {onSubjectChange ? (
        <FormField
          className="replyfield grid gap-[.3rem] mb-[.55rem] text-[.74rem] text-muted"
          label="Subject"
        >
          <TextInput
            className="replyinput block w-full rounded-md border border-line bg-bg px-[.55rem] py-[.45rem] text-[.82rem] leading-[1.45] text-fg resize-y disabled:opacity-[.55]"
            required
            value={subject ?? ""}
            disabled={busy}
            onChange={(e) => onSubjectChange(e.target.value)}
          />
        </FormField>
      ) : null}
      <TextArea
        className="replyinput block w-full rounded-md border border-line bg-bg px-[.55rem] py-[.45rem] text-[.82rem] leading-[1.45] text-fg resize-y disabled:opacity-[.55]"
        aria-label={mode === "reply" ? "Your reply" : "Message"}
        required={mode === "compose"}
        rows={4}
        value={body}
        disabled={busy}
        onChange={(e) => onBodyChange(e.target.value)}
      />
    </>
  );
}
