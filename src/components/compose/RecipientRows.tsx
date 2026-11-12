import type { ReactNode } from "react";
import AddressField, { type Address } from "./AddressField";
import type { Draft } from "./Draft";
import HeaderRow from "./HeaderRow";

export default function RecipientRows({
  draft,
  onDraft,
  onRecipientQuery,
  from,
  target,
  suggestions,
  mine,
  busy,
}: {
  draft: Draft;
  onDraft: (patch: Partial<Draft>) => void;
  onRecipientQuery?: (field: "to" | "cc", query: string) => void;
  from: ReactNode;
  target?: ReactNode;
  suggestions: Address[];
  mine: string[];
  busy: boolean;
}) {
  return (
    <>
      <HeaderRow label="from:">
        <span className="flex w-full min-w-0 items-start justify-between gap-1">
          {from}
          {target}
        </span>
      </HeaderRow>
      <HeaderRow label="to:">
        <AddressField
          onPendingQuery={(query) => onRecipientQuery?.("to", query)}
          label="to"
          value={draft.to}
          onChange={(to) => onDraft({ to })}
          suggestions={suggestions}
          taken={draft.cc}
          mine={mine}
          disabled={busy}
        />
      </HeaderRow>
      <HeaderRow label="cc:">
        <AddressField
          onPendingQuery={(query) => onRecipientQuery?.("cc", query)}
          label="cc"
          value={draft.cc}
          onChange={(cc) => onDraft({ cc })}
          suggestions={suggestions}
          taken={draft.to}
          mine={mine}
          disabled={busy}
        />
      </HeaderRow>
    </>
  );
}
