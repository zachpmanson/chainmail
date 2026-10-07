import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { $api } from "../../lib/api/api";
import { Button } from "../ui/controls";
import { TextInput } from "../ui/fields";
import InlineAlert from "../ui/InlineAlert";
import Consequence from "./Consequence";
import RuleRow, { type Draft } from "./RuleRow";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

type Phase =
  | { kind: "idle" }
  | { kind: "previewing"; draft: Draft }
  | { kind: "confirming"; draft: Draft }
  | { kind: "saving" };

export default function OpsOrgs() {
  const qc = useQueryClient();
  const orgs = $api.useQuery("get", "/v1/ops/orgs", {});
  const [phase, setPhase] = useState<Phase>({ kind: "idle" });
  const [newDomain, setNewDomain] = useState("");
  const [newOrg, setNewOrg] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [last, setLast] = useState<string | null>(null);

  const preview = $api.useMutation("post", "/v1/ops/orgs/preview");
  const save = $api.useMutation("post", "/v1/ops/orgs", {
    onError: (e) => setError(errText(e)),
  });

  async function pick(next: Draft) {
    setError(null);
    setLast(null);
    setPhase({ kind: "previewing", draft: next });
    try {
      await preview.mutateAsync({ body: { domain: next.domain, org: next.org ?? undefined } });
      setPhase({ kind: "confirming", draft: next });
    } catch (e) {
      setError(errText(e));
      setPhase({ kind: "idle" });
    }
  }

  async function apply() {
    if (phase.kind !== "confirming") return;
    const d = phase.draft;
    setPhase({ kind: "saving" });
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
    setPhase({ kind: "idle" });
    setNewDomain("");
    setNewOrg("");
    await qc.invalidateQueries({ queryKey: ["get", "/v1/ops/orgs"] });
  }

  const data = orgs.data;
  const busy = phase.kind === "previewing" || phase.kind === "saving";
  const draft = phase.kind === "previewing" || phase.kind === "confirming" ? phase.draft : null;

  return (
    <>
      <p className="my-1.5 mb-2 text-xs text-muted">
        A bubble is coloured by its sender's organisation. The corpus reads one from the mail
        domain; where that reads wrong, write the name here — two domains with one name are one
        organisation, and a domain you leave empty is nobody's.
      </p>
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      {last ? (
        <p className="my-1.5 mb-2 text-xs text-muted">{last}. The list below is the current one.</p>
      ) : null}

      {draft ? (
        <div className="mt-2 mb-0.5 rounded-md border border-line bg-quote p-2 text-xs leading-normal text-fg">
          <Consequence
            shift={preview.data ?? { messages: 0, people: 0, ambiguous: 0 }}
            draft={draft}
          />
          <div className="mt-2 flex items-center gap-2">
            <Button
              type="button"
              variant="danger"
              density="compact"
              disabled={phase.kind !== "confirming"}
              onClick={apply}
            >
              {draft.org === null ? "drop the rule" : "save this grouping"}
            </Button>
            <Button
              type="button"
              variant="subtle"
              density="compact"
              disabled={busy}
              onClick={() => setPhase({ kind: "idle" })}
            >
              cancel
            </Button>
          </div>
        </div>
      ) : null}

      {!data ? (
        <p className="my-1.5 mb-2 text-xs text-muted">
          {orgs.isPending ? "Reading the domains…" : "No domains."}
        </p>
      ) : data.domains.length === 0 ? (
        <p className="my-1.5 mb-2 text-xs text-muted">
          No mail has arrived with a domain of its own, so there is nothing to colour yet — but a
          rule written below will apply when it does.
        </p>
      ) : (
        <ol className="mt-2 list-none p-0">
          {data.domains.map((d) => (
            <li
              key={d.domain}
              className="not-first:mt-2 rounded-lg border border-line bg-card px-3 py-2"
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
