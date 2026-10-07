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
  onSubmit,
  onClose,
}: {
  draft: Draft;
  onDraft: (patch: Partial<Draft>) => void;
  accountId: string;
  onAccount: (accountId: string) => void;
  busy: boolean;
  onSubmit: (event: FormEvent) => void;
  onClose: () => void;
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
      <form ref={form} onSubmit={onSubmit}>
        <ComposerFields
          mode={{ kind: "compose" }}
          from={
            <AccountSelect value={accountId} disabled={busy} alwaysOfferNone onChange={onAccount} />
          }
          draft={draft}
          onDraft={onDraft}
          suggestions={suggestions}
          busy={busy}
        />
      </form>
      <footer className="mt-2 flex items-center justify-end gap-2">
        <Button variant="subtle" density="compact" type="button" disabled={busy} onClick={onClose}>
          Cancel
        </Button>
        <Button
          variant="subtle"
          density="compact"
          type="button"
          disabled={busy || draft.to.length === 0}
          onClick={() => form.current?.requestSubmit()}
        >
          {busy ? "Preparing…" : "Review message"}
        </Button>
      </footer>
    </>
  );
}
