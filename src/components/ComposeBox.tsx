import { useMemo, useRef, useState, type FormEvent } from "react";
import { useSearch } from "@tanstack/react-router";
import { $api } from "../lib/api";
import { usePersonAddresses } from "../lib/who";
import { addressWords, type Address } from "./AddressField";
import { ComposerFields } from "./ComposerFields";
import { ComposerFlow } from "./ComposerFlow";
import { SelectInput } from "./controls";

type ComposeResult = {
  accountId: string;
  to: string;
  cc?: string;
  subject: string;
  body: string;
  sent: boolean;
  gmailId?: string;
  filed?: boolean;
};

type Props = { onClose: () => void };

async function compose(input: {
  to: string[];
  cc: string[];
  subject: string;
  body: string;
  confirm: boolean;
  accountId?: string;
}): Promise<ComposeResult> {
  const response = await fetch("/v1/compose", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  const result = await response.json();
  if (!response.ok) throw new Error(result.error ?? `${response.status} ${response.statusText}`);
  return result;
}

export function ComposeBox({ onClose }: Props) {
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
  const [preview, setPreview] = useState<ComposeResult | null>(null);
  const [result, setResult] = useState<ComposeResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [sendFailed, setSendFailed] = useState(false);
  const [error, setError] = useState("");
  const to = recipients.map(addressWords);
  const ccRecipients = cc.map(addressWords);

  async function prepare(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      setPreview(
        await compose({
          to,
          cc: ccRecipients,
          subject,
          body,
          confirm: false,
          ...(accountId ? { accountId } : {}),
        }),
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function send() {
    if (
      !preview ||
      preview.to !== to.join(", ") ||
      preview.cc !== (ccRecipients.length ? ccRecipients.join(", ") : undefined) ||
      preview.subject !== subject ||
      preview.body !== body
    )
      return;
    setBusy(true);
    setError("");
    try {
      setResult(
        await compose({
          to,
          cc: ccRecipients,
          subject,
          body,
          confirm: true,
          accountId: preview.accountId,
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

  const isPreview = preview !== null && result === null;
  const editor = (
    <form ref={form} onSubmit={prepare}>
      <ComposerFields
        mode="compose"
        from={
          <div className="replyrecipient contents min-w-0 compose-account">
            <span className="replylabel inline-flex h-[var(--addrrow)] items-center">from:</span>
            <SelectInput
              className="replyfrom h-[var(--addrrow)] min-w-0 flex-1 rounded-md border border-line bg-bg px-[.3rem] text-[.72rem] text-fg focus:border-accent disabled:cursor-default disabled:opacity-[.55]"
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

  return (
    <ComposerFlow
      variant="compose"
      step={result ? "done" : isPreview ? "preview" : "compose"}
      busy={busy}
      error={
        error ? (
          <>
            {error}
            {sendFailed ? " Check Gmail before attempting to send again." : ""}
          </>
        ) : undefined
      }
      editor={editor}
      preview={
        preview ? (
          <dl className="compose-review grid grid-cols-[5rem_minmax(0,1fr)] gap-2 [overflow-wrap:anywhere]">
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
            <p className="col-span-full">
              Review the exact message above. Sending is irreversible.
            </p>
          </dl>
        ) : null
      }
      done={
        result ? (
          <>
            <p>
              Sent to {result.to}
              {result.cc ? `, cc ${result.cc}` : ""}.
            </p>
            <p>
              {result.filed
                ? "Filed in the corpus."
                : "Sent successfully, but could not be filed in the corpus."}
            </p>
          </>
        ) : null
      }
      onReview={() => form.current?.requestSubmit()}
      onEdit={() => {
        if (sendFailed) onClose();
        else {
          setPreview(null);
          setError("");
        }
      }}
      showConfirm={!sendFailed}
      editLabel={sendFailed ? "Close" : "Edit"}
      onConfirm={() => void send()}
      onClose={onClose}
      reviewDisabled={recipients.length === 0}
      confirmDisabled={
        sendFailed ||
        !preview ||
        preview.to !== to.join(", ") ||
        preview.cc !== (ccRecipients.length ? ccRecipients.join(", ") : undefined) ||
        preview.subject !== subject ||
        preview.body !== body
      }
      reviewLabel="Review message"
      confirmLabel="Confirm and send"
    />
  );
}
