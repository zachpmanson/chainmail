import { useMemo, useRef, useState, type FormEvent } from "react";
import { useSearch } from "@tanstack/react-router";
import { $api, type ComposeResponse } from "../../lib/api/api";
import { usePersonAddresses } from "../../lib/message/who";
import { addressWords, type Address } from "./AddressField";
import ComposerFields from "./ComposerFields";
import { SelectInput } from "../ui/fields";
import { Button } from "../ui/controls";
import InlineAlert from "../ui/InlineAlert";

export default function ComposeBox({ onClose }: { onClose: () => void }) {
  const compose = $api.useMutation("post", "/v1/compose");
  const routeAccountId = useSearch({ from: "/" }).accountId;
  const [accountId, setAccountId] = useState(routeAccountId ?? "");
  const accounts = $api.useQuery("get", "/auth/status", {});
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
  const form = useRef<HTMLFormElement>(null);
  const [recipients, setRecipients] = useState<Address[]>([]);
  const [cc, setCc] = useState<Address[]>([]);
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [preview, setPreview] = useState<ComposeResponse | null>(null);
  const [result, setResult] = useState<ComposeResponse | null>(null);
  const [busy, setBusy] = useState(false);
  const [sendFailed, setSendFailed] = useState(false);
  const [error, setError] = useState("");
  const to = recipients.map(addressWords);
  const ccRecipients = cc.map(addressWords);
  // Sending is only allowed for exactly what the reader reviewed.
  const previewIsCurrent =
    preview !== null &&
    preview.to === to.join(", ") &&
    preview.cc === (ccRecipients.length ? ccRecipients.join(", ") : undefined) &&
    preview.subject === subject &&
    preview.body === body;

  async function prepare(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      setPreview(
        await compose.mutateAsync({
          body: {
            to,
            cc: ccRecipients,
            subject,
            body,
            confirm: false,
            ...(accountId ? { accountId } : {}),
          },
        }),
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function send() {
    if (!preview || !previewIsCurrent) return;
    setBusy(true);
    setError("");
    try {
      setResult(
        await compose.mutateAsync({
          body: {
            to,
            cc: ccRecipients,
            subject,
            body,
            confirm: true,
            accountId: preview.accountId,
          },
        }),
      );
    } catch (e) {
      // The request may have reached Gmail even if its response did not. Avoid a
      // retry that could duplicate mail; the reader must check Gmail first.
      setSendFailed(true);
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  const editor = (
    <form ref={form} onSubmit={prepare}>
      <ComposerFields
        mode="compose"
        from={
          <div className="contents min-w-0">
            <span className="inline-flex h-7 items-center">from:</span>
            <SelectInput
              className="h-7 min-w-0 flex-1 rounded-md border border-line bg-bg px-1 text-[.72rem] text-fg focus:border-accent disabled:cursor-default disabled:opacity-[.55]"
              aria-label="From"
              value={accountId}
              disabled={busy}
              onChange={(event) => {
                setAccountId(event.target.value);
                setPreview(null);
              }}
            >
              <option value="">Choose account</option>
              {(accounts.data?.accounts ?? [])
                .filter((account) => account.signedIn)
                .map((account) => (
                  <option key={account.id} value={account.id}>
                    {account.displayName}
                    {account.email ? ` (${account.email})` : ""}
                  </option>
                ))}
            </SelectInput>
          </div>
        }
        to={recipients}
        onToChange={setRecipients}
        cc={cc}
        onCcChange={setCc}
        suggestions={suggestions}
        editingRecipients={true}
        subject={subject}
        onSubjectChange={setSubject}
        body={body}
        onBodyChange={setBody}
        busy={busy}
      />
    </form>
  );

  const errorAlert = error ? (
    <InlineAlert>
      {error}
      {sendFailed ? " Check Gmail before attempting to send again." : ""}
    </InlineAlert>
  ) : null;

  let content;
  if (result) {
    content = (
      <div role="status">
        <p>
          Sent to {result.to}
          {result.cc ? `, cc ${result.cc}` : ""}.
        </p>
        <p>
          {result.filed
            ? "Filed in the corpus."
            : "Sent successfully, but could not be filed in the corpus."}
        </p>
        <Button variant="subtle" density="compact" type="button" onClick={onClose}>
          Done
        </Button>
      </div>
    );
  } else if (preview) {
    content = (
      <>
        <dl className="grid grid-cols-[5rem_minmax(0,1fr)] gap-2 [overflow-wrap:anywhere]">
          <dt className="font-bold">Sending account</dt>
          <dd className="m-0">
            {accounts.data?.accounts?.find((account) => account.id === preview.accountId)
              ?.displayName ?? preview.accountId}
          </dd>
          <dt className="font-bold">To</dt>
          <dd className="m-0">{preview.to}</dd>
          {preview.cc ? (
            <>
              <dt className="font-bold">Cc</dt>
              <dd className="m-0">{preview.cc}</dd>
            </>
          ) : null}
          <dt className="font-bold">Subject</dt>
          <dd className="m-0">{preview.subject}</dd>
          <dt className="font-bold">Plain-text message</dt>
          <dd className="m-0">
            <pre className="m-0 whitespace-pre-wrap [overflow-wrap:anywhere] [font:inherit]">
              {preview.body}
            </pre>
          </dd>
          <p className="col-span-full">Review the exact message above. Sending is irreversible.</p>
        </dl>
        {errorAlert}
        <footer className="mt-2 flex items-center justify-end gap-2">
          {sendFailed ? (
            <Button variant="subtle" density="compact" type="button" onClick={onClose}>
              Close
            </Button>
          ) : (
            <>
              <Button
                variant="subtle"
                density="compact"
                type="button"
                disabled={busy}
                onClick={() => {
                  setPreview(null);
                  setError("");
                }}
              >
                Edit
              </Button>
              <Button
                variant="danger"
                density="compact"
                type="button"
                disabled={busy || !previewIsCurrent}
                onClick={() => void send()}
              >
                {busy ? "Sending…" : "Confirm and send"}
              </Button>
            </>
          )}
        </footer>
      </>
    );
  } else {
    content = (
      <>
        {errorAlert}
        {editor}
        <footer className="mt-2 flex items-center justify-end gap-2">
          <Button
            variant="subtle"
            density="compact"
            type="button"
            disabled={busy}
            onClick={onClose}
          >
            Cancel
          </Button>
          <Button
            variant="subtle"
            density="compact"
            type="button"
            disabled={busy || recipients.length === 0}
            onClick={() => form.current?.requestSubmit()}
          >
            {busy ? "Preparing…" : "Review message"}
          </Button>
        </footer>
      </>
    );
  }

  return (
    <aside
      className="ibread flex min-w-0 flex-col overflow-auto border-l border-line bg-bg min-[60rem]:h-full min-[60rem]:min-h-0 min-[60rem]:px-5"
      aria-label="Compose email"
    >
      <div className="mt-4 mb-1 rounded-lg border border-line bg-card px-3 py-2">{content}</div>
    </aside>
  );
}
