import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { $api, type OrgRule } from "../../lib/api/api";
import StatusBadge from "../ui/StatusBadge";
import { Button } from "../ui/controls";
import { TextInput } from "../ui/fields";
import InlineAlert from "../ui/InlineAlert";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/** `org: null` drops the rule; `""` rules the domain is no organisation. */
type Draft = { domain: string; org: string | null };

function Consequence({
  shift,
  draft,
}: {
  shift: { messages: number; people: number; ambiguous: number };
  draft: Draft;
}) {
  const { messages, people, ambiguous } = shift;
  return (
    <p>
      {draft.org === null ? (
        <>
          <strong>{draft.domain}</strong> goes back to being read from its own name.
        </>
      ) : draft.org === "" ? (
        <>
          <strong>{draft.domain}</strong> is not an organisation — its mail leaves whatever grouping
          it is in and takes the unknown colour.
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

function RuleRow({
  d,
  busy,
  onPick,
}: {
  d: OrgRule;
  busy: boolean;
  onPick: (draft: Draft) => void;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  const value = draft ?? d.org ?? "";
  const edited = draft !== null && draft !== (d.org ?? "");
  const mail = `${d.messages} message${d.messages === 1 ? "" : "s"} from ${d.people} sender${
    d.people === 1 ? "" : "s"
  }`;
  return (
    <article>
      <p className="mt-0 mb-1.5 text-[.68rem] font-bold uppercase tracking-[.05em] text-muted">
        <code>{d.domain}</code>
        {d.stored ? (
          <StatusBadge tone="success" className="ml-2">
            yours
          </StatusBadge>
        ) : (
          <StatusBadge tone="neutral">guessed</StatusBadge>
        )}
      </p>
      <p className="my-0.5 text-[.84rem] leading-[1.35] [&_code]:text-[.74rem] [&_code]:[overflow-wrap:anywhere]">
        {mail} — drawn as{" "}
        {d.org ? <code>{d.org}</code> : <span className="text-muted">no organisation</span>}
        {d.stored && d.guess ? (
          <span className="text-muted"> — cleared, it is read as {d.guess}</span>
        ) : null}
      </p>
      <p className="my-0.5 text-[.84rem] leading-[1.35] [&_code]:text-[.74rem] [&_code]:[overflow-wrap:anywhere]">
        <TextInput
          className="min-w-0 flex-[0_1_12rem] mr-2 px-1.5 py-1 text-xs"
          value={value}
          disabled={busy}
          placeholder="no rule"
          aria-label={`Organisation for ${d.domain}`}
          onChange={(e) => setDraft(e.target.value)}
        />
        <Button
          type="button"
          variant="subtle"
          density="compact"
          disabled={busy || !edited}
          onClick={() => {
            setDraft(null);
            onPick({ domain: d.domain, org: value.trim() });
          }}
        >
          {value.trim() === "" ? "save — nobody's" : "save"}
        </Button>
        {d.stored ? (
          <Button
            type="button"
            variant="subtle"
            density="compact"
            disabled={busy}
            onClick={() => {
              setDraft(null);
              onPick({ domain: d.domain, org: null });
            }}
          >
            clear
          </Button>
        ) : null}
      </p>
    </article>
  );
}

export default function OpsOrgs() {
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
      // The mutation's error is already on screen.
    }
    setBusy(false);
    setNewDomain("");
    setNewOrg("");
    await qc.invalidateQueries({ queryKey: ["get", "/v1/ops/orgs"] });
  }

  const data = orgs.data;

  return (
    <>
      <p className="my-1.5 mb-2 text-[.74rem] text-muted">
        A bubble is coloured by its sender's organisation. The corpus reads one from the mail
        domain; where that reads wrong, write the name here — two domains with one name are one
        organisation, and a domain you leave empty is nobody's.
      </p>
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      {last ? (
        <p className="my-1.5 mb-2 text-[.74rem] text-muted">
          {last}. The list below is the current one.
        </p>
      ) : null}

      {draft ? (
        <div className="mt-2 mb-0.5 rounded-md border border-line bg-quote px-2 py-2 text-[.76rem] leading-[1.5] text-fg">
          <Consequence
            shift={preview.data ?? { messages: 0, people: 0, ambiguous: 0 }}
            draft={draft}
          />
          <div className="mt-2 flex items-center gap-2">
            <Button
              type="button"
              variant="danger"
              density="compact"
              disabled={busy || preview.isPending}
              onClick={apply}
            >
              {draft.org === null ? "drop the rule" : "save this grouping"}
            </Button>
            <Button
              type="button"
              variant="subtle"
              density="compact"
              disabled={busy}
              onClick={() => setDraft(null)}
            >
              cancel
            </Button>
          </div>
        </div>
      ) : null}

      {!data ? (
        <p className="my-1.5 mb-2 text-[.74rem] text-muted">
          {orgs.isPending ? "Reading the domains…" : "No domains."}
        </p>
      ) : data.domains.length === 0 ? (
        <p className="my-1.5 mb-2 text-[.74rem] text-muted">
          No mail has arrived with a domain of its own, so there is nothing to colour yet — but a
          rule written below will apply when it does.
        </p>
      ) : (
        <ol className="mt-2 list-none p-0">
          {data.domains.map((d) => (
            <li
              key={d.domain}
              className="not-first:mt-2 rounded-[9px] border border-line bg-card px-3 py-2"
            >
              <RuleRow d={d} busy={busy} onPick={pick} />
            </li>
          ))}
        </ol>
      )}
      <form
        className="mt-2 flex flex-wrap items-center gap-1.5 border-t border-line pt-2"
        onSubmit={(e) => {
          e.preventDefault();
          const domain = newDomain.trim().toLowerCase();
          if (domain === "") return;
          void pick({ domain, org: newOrg.trim() });
        }}
      >
        <TextInput
          className="min-w-0 flex-[0_1_12rem] mr-0 px-1.5 py-1 text-xs"
          value={newDomain}
          disabled={busy}
          placeholder="a domain with no mail yet, e.g. termina.io"
          aria-label="Domain to rule on"
          onChange={(e) => setNewDomain(e.target.value)}
        />
        <TextInput
          className="min-w-0 flex-[0_1_12rem] mr-0 px-1.5 py-1 text-xs"
          value={newOrg}
          disabled={busy}
          placeholder="the organisation, empty for none"
          aria-label="Organisation for that domain"
          onChange={(e) => setNewOrg(e.target.value)}
        />
        <Button
          type="submit"
          variant="subtle"
          density="compact"
          disabled={busy || newDomain.trim() === ""}
        >
          rule on this domain
        </Button>
      </form>
    </>
  );
}
