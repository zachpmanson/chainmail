import type { ReactNode } from "react";
import TextArea from "../ui/TextArea";
import type { Address } from "./AddressField";
import type { Draft } from "./Draft";
import HeaderRow from "./HeaderRow";
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
        {mode.kind === "compose" ? (
          <HeaderRow label="subject:">
            {/* AddressField's box, so the subject reads as another header line. */}
            <input
              className="mx-0.5 min-h-7 w-full min-w-0 rounded-md border border-line bg-bg px-1 py-0.5 font-[inherit] text-xs text-fg outline-none placeholder:text-muted focus:border-accent disabled:opacity-55"
              type="text"
              aria-label="Subject"
              required
              value={draft.subject}
              disabled={busy}
              onChange={(e) => onDraft({ subject: e.target.value })}
            />
          </HeaderRow>
        ) : null}
      </div>
      <TextArea
        className="block w-full resize-y rounded-md border border-line bg-bg p-2 text-sm/normal text-fg disabled:opacity-55"
        aria-label={mode.kind === "reply" ? "Your reply" : "Message"}
        placeholder={mode.kind === "reply" ? "Write your reply…" : "Write your message…"}
        required={mode.kind === "compose"}
        rows={4}
        value={draft.body}
        disabled={busy}
        onChange={(e) => onDraft({ body: e.target.value })}
      />
    </>
  );
}
