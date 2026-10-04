import { useMemo, useRef, useState, type FormEvent } from "react";
import { usePersonAddresses } from "../lib/who";
import { addressWords, type Address } from "./AddressField";
import { ComposerFields } from "./ComposerFields";
import { ComposerFlow } from "./ComposerFlow";

type ComposeResult = {
  to: string;
  subject: string;
  body: string;
  sent: boolean;
  gmailId?: string;
  filed?: boolean;
};

type Props = { onClose: () => void };

async function compose(input: { to: string; subject: string; body: string; confirm: boolean }): Promise<ComposeResult> {
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
  const people = usePersonAddresses();
  const suggestions = useMemo<Address[]>(() => {
    const seen = new Set<string>();
    const out: Address[] = [];
    for (const addresses of people.values()) for (const address of addresses) {
      const key = address.toLowerCase();
      if (!seen.has(key)) { seen.add(key); out.push({ address }); }
    }
    return out;
  }, [people]);
  const form = useRef<HTMLFormElement>(null);
  const [recipients, setRecipients] = useState<Address[]>([]);
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [preview, setPreview] = useState<ComposeResult | null>(null);
  const [result, setResult] = useState<ComposeResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [sendFailed, setSendFailed] = useState(false);
  const [error, setError] = useState("");
  const to = recipients.map(addressWords).join(", ");

  async function prepare(event: FormEvent) {
    event.preventDefault();
    setBusy(true); setError("");
    try { setPreview(await compose({ to, subject, body, confirm: false })); }
    catch (e) { setError(e instanceof Error ? e.message : String(e)); }
    finally { setBusy(false); }
  }

  async function send() {
    if (!preview || preview.to !== to || preview.subject !== subject || preview.body !== body) return;
    setBusy(true); setError("");
    try { setResult(await compose({ to, subject, body, confirm: true })); }
    catch (e) {
      // The request may have reached Gmail even if its response did not. Avoid a
      // retry that could duplicate mail; the reader must check Gmail first.
      setSendFailed(true);
      setError(e instanceof Error ? e.message : String(e));
    }
    finally { setBusy(false); }
  }

  const isPreview = preview !== null && result === null;
  const editor = (
    <form ref={form} onSubmit={prepare}>
      <ComposerFields
        mode="compose"
        to={recipients}
        onToChange={setRecipients}
        suggestions={suggestions}
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
      title={result ? "Message sent" : isPreview ? "Review email" : "Compose email"}
      busy={busy}
      error={error ? <>{error}{sendFailed ? " Check Gmail before attempting to send again." : ""}</> : undefined}
      editor={editor}
      preview={preview ? <dl className="compose-review"><dt>To</dt><dd>{preview.to}</dd><dt>Subject</dt><dd>{preview.subject}</dd><dt>Plain-text message</dt><dd><pre>{preview.body}</pre></dd><p>Review the exact message above. Sending is irreversible.</p></dl> : null}
      done={result ? <><p>Sent to {result.to}.</p><p>{result.filed ? "Filed in the corpus." : "Sent successfully, but could not be filed in the corpus."}</p></> : null}
      onReview={() => form.current?.requestSubmit()}
      onEdit={() => { if (sendFailed) onClose(); else { setPreview(null); setError(""); } }}
      showConfirm={!sendFailed}
      editLabel={sendFailed ? "Close" : "Edit"}
      onConfirm={() => void send()}
      onClose={onClose}
      reviewDisabled={recipients.length === 0}
      confirmDisabled={sendFailed || !preview || preview.to !== to || preview.subject !== subject || preview.body !== body}
      reviewLabel="Review message"
      confirmLabel="Confirm and send"
    />
  );
}
