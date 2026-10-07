import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { $api, type PersonSummary } from "../../lib/api/api";
import FormField from "../ui/FormField";
import { TextInput } from "../ui/fields";
import InlineAlert from "../ui/InlineAlert";
import PersonRow from "./PersonRow";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

const SHOWN = 25;

function haystack(p: PersonSummary): string {
  return [p.displayName, ...(p.identities ?? [])].join(" ").toLowerCase();
}

export default function OpsPeople() {
  const qc = useQueryClient();
  const people = $api.useQuery("get", "/v1/people");
  const save = $api.useMutation("post", "/v1/people/{personId}");
  const [q, setQ] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [last, setLast] = useState<string | null>(null);

  const all = people.data?.people;
  const shown = useMemo(() => {
    const entries = all ?? [];
    const needle = q.trim().toLowerCase();
    const hits = needle === "" ? entries : entries.filter((p) => haystack(p).includes(needle));
    return { hits, capped: hits.slice(0, SHOWN), more: Math.max(0, hits.length - SHOWN) };
  }, [all, q]);

  async function edit(
    id: number,
    name: string,
    change: { displayName?: string; addIdentities?: string[]; removeIdentities?: string[] },
  ) {
    setError(null);
    setLast(null);
    setBusy(true);
    try {
      await save.mutateAsync({ params: { path: { personId: id } }, body: change });
      setLast(
        change.displayName
          ? `#${id} is called ${change.displayName}`
          : change.addIdentities
            ? `${change.addIdentities.join(", ")} is #${id}'s`
            : `${change.removeIdentities?.join(", ")} is no longer #${id}'s`,
      );
    } catch (e) {
      setError(`${name}: ${errText(e)}`);
    }
    setBusy(false);
    await qc.invalidateQueries({ queryKey: ["get", "/v1/people"] });
    await qc.invalidateQueries({ queryKey: ["get", "/v1/ops/plan"] });
  }

  return (
    <>
      <p className="my-1.5 mb-2 text-xs text-muted">
        Everyone the corpus knows, and every address that resolves to them. The mail client reads
        its senders from here, so correcting a name or an address here corrects it wherever that
        person appears — in the list, in a thread, in the person field of the search. Two rows that
        are one human are one row after a merge, and that is the plan below, where the evidence is.
      </p>
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      {last ? (
        <p className="my-1.5 mb-2 text-xs text-muted">{last}. The list below is the current one.</p>
      ) : null}
      <FormField className="my-2 mb-1 flex items-center gap-1.5 text-xs" label="Find a person">
        <TextInput
          className="min-w-0 flex-[0_1_18rem] mr-0 px-1.5 py-1 text-xs"
          value={q}
          placeholder="a name or an address"
          aria-label="Find a person"
          onChange={(e) => setQ(e.target.value)}
        />
      </FormField>
      {!people.data ? (
        <p className="my-1.5 mb-2 text-xs text-muted">
          {people.isPending ? "Reading the people…" : "No people."}
        </p>
      ) : shown.hits.length === 0 ? (
        <p className="my-1.5 mb-2 text-xs text-muted">
          Nobody matches {q.trim() === "" ? "—" : <code>{q.trim()}</code>}. A person the corpus has
          never seen cannot be added here: a row exists because the mail named them, and a hand-made
          row would be one no message could ever reach.
        </p>
      ) : (
        <ol className="mt-2 list-none p-0">
          {shown.capped.map((p) => (
            <li
              key={p.personId}
              className="not-first:mt-2 rounded-lg border border-line bg-card px-3 py-2"
            >
              <PersonRow
                p={p}
                busy={busy}
                onEdit={(change) => void edit(p.personId, p.displayName, change)}
              />
            </li>
          ))}
        </ol>
      )}
      {shown.more > 0 ? (
        <p className="my-1.5 mb-2 text-xs text-muted">
          {shown.more} more {shown.more === 1 ? "person" : "people"} match
          {q.trim() === "" ? "" : " that"} — keep typing to narrow it down.
        </p>
      ) : null}
    </>
  );
}
