import type { ReactNode } from "react";
import FormField from "../ui/FormField";
import { Button } from "../ui/controls";
import { TextArea, TextInput } from "../ui/fields";
import AddressField from "./AddressField";
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

export default function ComposerFields({
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
}: {
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
}) {
  const hasCc = onCcChange !== undefined;
  const expanded = editingRecipients;
  return (
    <>
      <div className="grid grid-cols-[max-content_minmax(0,1fr)_max-content] items-start gap-x-1 gap-y-1.5 mb-2 text-[.74rem] text-muted">
        {expanded ? from : null}
        <div className="contents min-w-0">
          {expanded ? (
            <>
              <span className="inline-flex h-7 items-center">to:</span>
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
              className="group col-span-2 flex h-7 min-w-0 flex-1 gap-1 overflow-hidden border-0 bg-transparent p-0 text-left font-[inherit] text-[.74rem] font-normal text-muted cursor-pointer hover:border-0 hover:bg-transparent hover:text-muted disabled:cursor-default disabled:opacity-[.55] focus-visible:rounded-[4px] focus-visible:outline focus-visible:outline-1 focus-visible:outline-offset-2 focus-visible:outline-accent"
              aria-expanded={false}
              aria-label={`Edit recipients — currently ${recipientWords(to, cc)}`}
              title="Edit recipients"
              disabled={busy}
              onClick={onEditRecipients}
            >
              <span className="inline-flex h-7 items-center">to:</span>
              <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap group-hover:text-fg">
                {to.length ? names(to) : null}
                {cc.length ? (
                  <>
                    <span>{to.length ? ", cc " : "cc "}</span>
                    {names(cc)}
                  </>
                ) : null}
                {!to.length && !cc.length ? <span className="italic">add an address</span> : null}
              </span>
            </Button>
          )}
          {target}
        </div>
        {expanded && hasCc ? (
          <div className="contents min-w-0">
            <span className="inline-flex h-7 items-center">cc:</span>
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
        <FormField className="grid gap-1 mb-2 text-[.74rem] text-muted" label="Subject">
          <TextInput
            className="block w-full rounded-md border border-line bg-bg px-2 py-2 text-[.82rem] leading-[1.45] text-fg resize-y disabled:opacity-[.55]"
            required
            value={subject ?? ""}
            disabled={busy}
            onChange={(e) => onSubjectChange(e.target.value)}
          />
        </FormField>
      ) : null}
      <TextArea
        className="block w-full rounded-md border border-line bg-bg px-2 py-2 text-[.82rem] leading-[1.45] text-fg resize-y disabled:opacity-[.55]"
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
