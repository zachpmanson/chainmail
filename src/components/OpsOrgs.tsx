import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { $api, type OrgRule } from "../lib/api";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/** One reader's edit, waiting on the confirm: the label they want a domain's mail
 *  drawn under, with `null` meaning "there should be no rule at all" — which is
 *  the third answer and not the same as an empty label. */
type Draft = { domain: string; org: string | null };

/** The consequence, in words, of the edit about to be saved. Nothing here is
 *  guessed: the counts come back from the resolver that will apply the rule. */
function Consequence({ shift, draft }: { shift: { messages: number; people: number; ambiguous: number }; draft: Draft }) {
  const { messages, people, ambiguous } = shift;
  return (
    <p className="opmwarn">
      {draft.org === null ? (
        <>
          <strong>{draft.domain}</strong> goes back to being read from its own name.
        </>
      ) : draft.org === "" ? (
        <>
          <strong>{draft.domain}</strong> is not an organisation — its mail leaves
          whatever grouping it is in and takes the unknown colour.
        </>
      ) : (
        <>
          <strong>{draft.domain}</strong> joins every other domain drawn as{" "}
          <strong>{draft.org}</strong>.
        </>
      )}{" "}
      {messages === 0
        ? "No message changes colour."
        : `${messages} message${messages === 1 ? "" : "s"} from ${people} sender${
            people === 1 ? "" : "s"
          } would be drawn differently.`}
      {ambiguous > 0
        ? ` ${ambiguous} more cannot be placed at all: ${
            ambiguous === 1 ? "its sender's" : "their senders'"
          } own mail names two organisations, and the entry has no address of its own.`
        : null}
    </p>
  );
}

/** One domain: how much mail is drawn from it, what that mail is drawn as now,
 *  and the edit. The three states are offered as the three acts they are —
 *  naming an organisation, saving an empty label (this is nobody's), and dropping
 *  the rule — because a text field alone can only tell the first from the others
 *  if the reader knows what an empty box means. */
function RuleRow({ d, busy, onPick }: { d: OrgRule; busy: boolean; onPick: (draft: Draft) => void }) {
  const [draft, setDraft] = useState<string | null>(null);
  const value = draft ?? d.org ?? "";
  const edited = draft !== null && draft !== (d.org ?? "");
  const mail = `${d.messages} message${d.messages === 1 ? "" : "s"} from ${d.people} sender${
    d.people === 1 ? "" : "s"
  }`;
  return (
    <article className="opmerge">
      <p className="opmrule">
        <code>{d.domain}</code>
        {d.stored ? <span className="opbad op-apply">yours</span> : <span className="opbad op-ro">guessed</span>}
      </p>
      <p className="opmside">
        {mail} — drawn as{" "}
        {d.org ? <code>{d.org}</code> : <span className="opwhy">no organisation</span>}
        {d.stored && d.guess ? (
          // What clearing the rule would restore. Without it, a reader who has
          // ruled on a domain cannot tell what the guess behind it was — and that
          // guess is the thing this screen exists to override or accept.
          <span className="opwhy"> — cleared, it is read as {d.guess}</span>
        ) : null}
      </p>
      <p className="opmside">
        <input
          className="oporginput"
          value={value}
          disabled={busy}
          placeholder="no rule"
          aria-label={`Organisation for ${d.domain}`}
          onChange={(e) => setDraft(e.target.value)}
        />
        <button
          type="button"
          className="opbtn"
          disabled={busy || !edited}
          onClick={() => {
            setDraft(null);
            onPick({ domain: d.domain, org: value.trim() });
          }}
        >
          {value.trim() === "" ? "save — nobody's" : "save"}
        </button>
        {d.stored ? (
          <button
            type="button"
            className="opbtn"
            disabled={busy}
            onClick={() => {
              setDraft(null);
              onPick({ domain: d.domain, org: null });
            }}
          >
            clear
          </button>
        ) : null}
      </p>
    </article>
  );
}

/**
 * The organisations half of /ops: which domain of mail belongs to whom, which is
 * what colours a bubble in the pane and on a page.
 *
 * The corpus guesses that from the domain itself, and the guess is only wrong
 * where a company mails from two domains or a domain's own name reads badly —
 * but a colour is a claim about the reader's correspondence, so the guess is a
 * default and their answer is the record. Choosing one writes a rule; the write
 * is behind the same kind of confirm as the merges above, and the number it shows
 * is counted by the resolver that will apply it, so the reader is agreeing to the
 * change the corpus will make rather than to an estimate of it.
 *
 * Domains are counted from the mail's own From header, so a domain with no mail
 * yet is absent from the list and can still be ruled on by hand — the field takes
 * anything, and a rule about mail that has not arrived is a thing an operator may
 * know first.
 */
export function OpsOrgs() {
  const qc = useQueryClient();
  const orgs = $api.useQuery("get", "/v1/ops/orgs", {});
  const [draft, setDraft] = useState<Draft | null>(null);
  const [newDomain, setNewDomain] = useState("");
  const [newOrg, setNewOrg] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [last, setLast] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const preview = $api.useMutation("post", "/v1/ops/orgs/preview");
  const save = $api.useMutation("post", "/v1/ops/orgs", {
    onError: (e) => setError(errText(e)),
  });

  // Every edit goes through the preview: the confirm needs the consequence, and a
  // screen that could save without one would be offering the reader a change they
  // have not been told the size of.
  async function pick(next: Draft) {
    setError(null);
    setLast(null);
    setDraft(next);
    setBusy(true);
    try {
      await preview.mutateAsync({ body: { domain: next.domain, org: next.org ?? undefined } });
    } catch (e) {
      setError(errText(e));
      setDraft(null);
    }
    setBusy(false);
  }

  async function apply() {
    if (!draft) return;
    const d = draft;
    setDraft(null);
    setBusy(true);
    try {
      await save.mutateAsync({ body: { domain: d.domain, org: d.org ?? undefined } });
      setLast(
        d.org === null
          ? `dropped the rule about ${d.domain}`
          : d.org === ""
            ? `${d.domain} is not an organisation`
            : `${d.domain} is drawn as ${d.org}`,
      );
    } catch {
      // The message is already on screen; the list below is refetched either way,
      // because a refusal is a statement about the corpus.
    }
    setBusy(false);
    setNewDomain("");
    setNewOrg("");
    await qc.invalidateQueries({ queryKey: ["get", "/v1/ops/orgs"] });
  }

  const data = orgs.data;

  return (
    <>
      <p className="opnote">
        A bubble is coloured by its sender's organisation. The corpus reads one
        from the mail domain; where that reads wrong, write the name here — two
        domains with one name are one organisation, and a domain you leave empty
        is nobody's.
      </p>
      {error ? (
        <p className="selfail" role="alert">
          {error}
        </p>
      ) : null}
      {last ? <p className="opnote">{last}. The list below is the current one.</p> : null}

      {draft ? (
        <div className="opmconfirm">
          <Consequence
            shift={preview.data ?? { messages: 0, people: 0, ambiguous: 0 }}
            draft={draft}
          />
          <div className="opmact">
            <button type="button" className="opbtn opbtn-after" disabled={busy || preview.isPending} onClick={apply}>
              {draft.org === null ? "drop the rule" : "save this grouping"}
            </button>
            <button type="button" className="opbtn" disabled={busy} onClick={() => setDraft(null)}>
              cancel
            </button>
          </div>
        </div>
      ) : null}

      {!data ? (
        <p className="opnote">{orgs.isPending ? "Reading the domains…" : "No domains."}</p>
      ) : data.domains.length === 0 ? (
        <p className="opnote">
          No mail has arrived with a domain of its own, so there is nothing to
          colour yet — but a rule written below will apply when it does.
        </p>
      ) : (
        <ol className="oplist">
          {data.domains.map((d) => (
            <li key={d.domain} className="oprow">
              <RuleRow d={d} busy={busy} onPick={pick} />
            </li>
          ))}
        </ol>
      )}
      <form
        className="oporgadd"
        onSubmit={(e) => {
          e.preventDefault();
          const domain = newDomain.trim().toLowerCase();
          if (domain === "") return;
          void pick({ domain, org: newOrg.trim() });
        }}
      >
        <input
          className="oporginput"
          value={newDomain}
          disabled={busy}
          placeholder="a domain with no mail yet, e.g. termina.io"
          aria-label="Domain to rule on"
          onChange={(e) => setNewDomain(e.target.value)}
        />
        <input
          className="oporginput"
          value={newOrg}
          disabled={busy}
          placeholder="the organisation, empty for none"
          aria-label="Organisation for that domain"
          onChange={(e) => setNewOrg(e.target.value)}
        />
        <button type="submit" className="opbtn" disabled={busy || newDomain.trim() === ""}>
          rule on this domain
        </button>
      </form>
    </>
  );
}
