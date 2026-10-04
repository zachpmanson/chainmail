import { useMemo, useState, type FormEvent } from "react";
import { usePersonAddresses } from "../lib/who";
import { AddressField, addressWords, type Address } from "./AddressField";

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
      // A failed HTTP request may have reached Gmail even if the response did
      // not reach us. Never offer an immediate retry that could duplicate mail.
      setSendFailed(true);
      setError(e instanceof Error ? e.message : String(e));
    }
    finally { setBusy(false); }
  }

  return (
    <div className="compose-backdrop" role="presentation" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <section className="compose-box" role="dialog" aria-modal="true" aria-labelledby="compose-title">
        <header className="compose-head"><h2 id="compose-title">{result ? "Message sent" : preview ? "Review email" : "Compose email"}</h2><button className="opbtn" type="button" onClick={onClose} aria-label="Close">×</button></header>
        {result ? <div role="status"><p>Sent to {result.to}.</p><p>{result.filed ? "Filed in the corpus." : "Sent successfully, but could not be filed in the corpus."}</p><button className="opbtn" type="button" onClick={onClose}>Done</button></div> : preview ? <>
          <dl className="compose-review"><dt>To</dt><dd>{preview.to}</dd><dt>Subject</dt><dd>{preview.subject}</dd><dt>Plain-text message</dt><dd><pre>{preview.body}</pre></dd></dl>
          <p>Review the exact message above. Sending is irreversible.</p>
          {error && <p role="alert">{error}{sendFailed && " Check Gmail before attempting to send again."}</p>}
          <footer>{sendFailed ? <button className="opbtn" type="button" onClick={onClose}>Close</button> : <><button className="opbtn" type="button" disabled={busy} onClick={() => { setPreview(null); setError(""); }}>Edit</button><button className="opbtn" type="button" disabled={busy} onClick={send}>{busy ? "Sending…" : "Confirm and send"}</button></>}</footer>
        </> : <form onSubmit={prepare}>
          <div className="replyrecipients"><div className="replyrecipient"><span className="replylabel">To</span><AddressField label="to" value={recipients} onChange={setRecipients} suggestions={suggestions} disabled={busy} /></div></div>
          <label>Subject <input className="replyinput" required value={subject} onChange={(e) => setSubject(e.target.value)} /></label>
          <label>Message <textarea className="replyinput" required rows={10} value={body} onChange={(e) => setBody(e.target.value)} /></label>
          {error && <p role="alert">{error}</p>}
          <footer><button className="opbtn" type="button" onClick={onClose}>Cancel</button><button className="opbtn" type="submit" disabled={busy || recipients.length === 0}>{busy ? "Preparing…" : "Review message"}</button></footer>
        </form>}
      </section>
    </div>
  );
}
