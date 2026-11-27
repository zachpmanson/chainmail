import BodyWordCount from "./BodyWordCount";
import { useMemo, useRef, type FormEvent } from "react";
import { usePersonAddresses } from "../../lib/message/who";
import Button from "../ui/Button";
import AccountSelect from "./AccountSelect";
import type { Address } from "./AddressField";
import ComposerFields from "./ComposerFields";
import type { Draft } from "./Draft";

export default function ComposeForm({
  draft,
  onDraft,
  accountId,
  onAccount,
  busy,
  cancelDisabled = false,
  editingBlocked = false,
  checkingDraft = false,
  onSubmit,
  onClose,
  onDiscard,
  onStop,
  stopDisabled = false,
}: {
  draft: Draft;
  onDraft: (patch: Partial<Draft>) => void;
  accountId: string;
  onAccount: (accountId: string) => void;
  busy: boolean;
  cancelDisabled?: boolean;
  editingBlocked?: boolean;
  checkingDraft?: boolean;
  onSubmit: (event: FormEvent) => void;
  onClose: () => void;
  onDiscard?: () => void;
  onStop?: () => void;
  stopDisabled?: boolean;
}) {
  const form = useRef<HTMLFormElement>(null);
  const people = usePersonAddresses();
  const suggestions = useMemo<Address[]>(() => {
    const seen = new Set<string>();
    const out: Address[] = [];
    for (const addresses of people.values())
      for (const address of addresses) {
        const key = address.toLowerCase();
        if (!seen.has(key)) {
          seen.add(key);
          out.push({ address });
        }
      }
    return out;
  }, [people]);

  return (
    <>
      <form ref={form} onSubmit={onSubmit} aria-busy={checkingDraft || undefined}>
        <ComposerFields
          mode={{ kind: "compose" }}
          from={
            <AccountSelect
              value={accountId}
              disabled={busy || editingBlocked}
              alwaysOfferNone
              onChange={onAccount}
            />
          }
          draft={draft}
          onDraft={onDraft}
          suggestions={suggestions}
          busy={busy || editingBlocked}
        />
      </form>
      <footer className="mt-2 flex flex-wrap items-center justify-end gap-2">
        <BodyWordCount body={draft.body} />
        {onDiscard ? (
          <Button
            variant="danger"
            density="compact"
            type="button"
            disabled={busy}
            onClick={onDiscard}
          >
            Discard draft
          </Button>
        ) : null}
        {!onStop || stopDisabled ? (
          <Button
            variant="subtle"
            density="compact"
            type="button"
            disabled={busy || cancelDisabled}
            onClick={onClose}
          >
            Cancel
          </Button>
        ) : null}
        {onStop ? (
          <Button
            variant="subtle"
            density="compact"
            type="button"
            disabled={busy || cancelDisabled || stopDisabled}
            title="Close the editor after the current changes are saved to Gmail."
            onClick={onStop}
          >
            Stop editing draft
          </Button>
        ) : null}
        <Button
          variant="subtle"
          density="compact"
          type="button"
          disabled={busy || editingBlocked || draft.to.length === 0}
          onClick={() => form.current?.requestSubmit()}
        >
          {busy ? "Preparing…" : "Review message"}
        </Button>
      </footer>
    </>
  );
}
