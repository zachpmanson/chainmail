import type { ReactNode } from "react";
import FormField from "../ui/FormField";
import { TextArea, TextInput } from "../ui/fields";
import type { Address } from "./AddressField";
import type { Draft } from "./Draft";
import RecipientRows from "./RecipientRows";
import RecipientSummary from "./RecipientSummary";

export default function ComposerFields({
  draft,
  onDraft,
  mode,
  from,
  suggestions,
  mine = [],
  busy,
}: {
  draft: Draft;
  onDraft: (patch: Partial<Draft>) => void;
  /** Compose always shows the recipient rows and a subject; reply collapses its rows behind a summary. */
  mode:
    | { kind: "compose" }
    | {
        kind: "reply";
        editingRecipients: boolean;
        onEditRecipients: () => void;
        target: ReactNode;
      };
  from: ReactNode;
  suggestions: Address[];
  mine?: string[];
  busy: boolean;
}) {
  return (
    <>
      <div className="mb-2 flex flex-col gap-1.5 text-xs text-muted">
        {mode.kind === "reply" && !mode.editingRecipients ? (
          <RecipientSummary
            to={draft.to}
            cc={draft.cc}
            busy={busy}
            onEdit={mode.onEditRecipients}
            target={mode.target}
          />
        ) : (
          <RecipientRows
            draft={draft}
            onDraft={onDraft}
            from={from}
            target={mode.kind === "reply" ? mode.target : undefined}
            suggestions={suggestions}
            mine={mine}
            busy={busy}
          />
        )}
      </div>
      {mode.kind === "compose" ? (
        <FormField className="flex flex-col gap-1 mb-2 text-xs text-muted" label="Subject">
          <TextInput
            className="block w-full rounded-md border border-line bg-bg p-2 text-sm leading-normal text-fg resize-y disabled:opacity-[.55]"
            required
            value={draft.subject}
            disabled={busy}
            onChange={(e) => onDraft({ subject: e.target.value })}
          />
        </FormField>
      ) : null}
      <TextArea
        className="block w-full rounded-md border border-line bg-bg p-2 text-sm leading-normal text-fg resize-y disabled:opacity-[.55]"
        aria-label={mode.kind === "reply" ? "Your reply" : "Message"}
        required={mode.kind === "compose"}
        rows={4}
        value={draft.body}
        disabled={busy}
        onChange={(e) => onDraft({ body: e.target.value })}
      />
    </>
  );
}
