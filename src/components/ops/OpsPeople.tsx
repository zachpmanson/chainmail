import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { $api, type PersonSummary } from "../../lib/api/api";
import FormField from "../ui/FormField";
import { Button } from "../ui/controls";
import { TextInput } from "../ui/fields";
import InlineAlert from "../ui/InlineAlert";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

const SHOWN = 25;

function haystack(p: PersonSummary): string {
  return [p.displayName, ...(p.identities ?? [])].join(" ").toLowerCase();
}

function asIdentity(typed: string): string {
  const t = typed.trim();
  if (t === "") return "";
  return t.includes(":") ? t : `email:${t}`;
}

function Identity({
  id,
  owner,
  busy,
  onDetach,
}: {
  id: string;
  owner: PersonSummary;
  busy: boolean;
  onDetach: (identity: string) => void;
}) {
  return (
    <span className="mr-1 mb-1 inline-flex items-center gap-0.5 rounded-[5px] border border-line bg-card px-1 py-px pr-1">
      <code className="text-[.72rem] [overflow-wrap:anywhere]">{id}</code>
      <Button
        type="button"
        variant="quiet"
        className="!size-auto !min-h-0 rounded px-1 py-0 text-[.85rem] leading-none text-muted hover:bg-quote hover:text-red-700 disabled:opacity-40"
        disabled={busy}
        title={`detach ${id}`}
        aria-label={`Detach ${id} from ${owner.displayName}`}
        onClick={() => onDetach(id)}
      >
        ×
      </Button>
    </span>
  );
}

/** The server refuses an identity another person already has: taking it is a
 *  merge, which belongs on the merge plan. */
function PersonRow({
  p,
  busy,
  onEdit,
}: {
  p: PersonSummary;
  busy: boolean;
  onEdit: (edit: {
    displayName?: string;
    addIdentities?: string[];
    removeIdentities?: string[];
  }) => void;
}) {
  const [name, setName] = useState<string | null>(null);
  const [add, setAdd] = useState("");
  const wanted = name ?? p.displayName;
  const renamed = name !== null && name.trim() !== "" && name.trim() !== p.displayName;
  const mail = `sent ${p.sent} · received ${p.received}`;
  return (
    <article>
      <p className="mt-0 mb-1.5 text-[.86rem] leading-[1.3]">
        <span className="text-muted">#{p.personId}</span>{" "}
        <span className="ml-0.5 font-semibold">{p.displayName}</span>{" "}
        <span className="text-muted">{mail}</span>
      </p>
      <form
        className="my-1 mb-0.5 flex flex-wrap items-center gap-1.5"
        onSubmit={(e) => {
          e.preventDefault();
          if (!renamed) return;
          onEdit({ displayName: wanted.trim() });
          setName(null);
        }}
      >
        <TextInput
          className="min-w-0 flex-[0_1_16rem] mr-0 px-1.5 py-1 text-xs"
          value={wanted}
          disabled={busy}
          aria-label={`Name for ${p.displayName}`}
          onChange={(e) => setName(e.target.value)}
        />
        <Button type="submit" variant="subtle" density="compact" disabled={busy || !renamed}>
          rename
        </Button>
      </form>
      <p className="my-0.5 text-[.84rem] leading-[1.35] [&_code]:text-[.74rem] [&_code]:[overflow-wrap:anywhere]">
        {(p.identities ?? []).length === 0 ? (
          <span className="text-muted">
            No identity at all — this one is only ever the name in someone else's header, which is
            why a name is the only way to find them.
          </span>
        ) : (
          (p.identities ?? []).map((id) => (
            <Identity
              key={id}
              id={id}
              owner={p}
              busy={busy}
              onDetach={(i) => onEdit({ removeIdentities: [i] })}
            />
          ))
        )}
      </p>
      <form
        className="mt-2 flex flex-wrap items-center gap-1.5 border-t border-line pt-2"
        onSubmit={(e) => {
          e.preventDefault();
          const id = asIdentity(add);
          if (id === "") return;
          onEdit({ addIdentities: [id] });
          setAdd("");
        }}
      >
        <TextInput
          className="min-w-0 flex-[0_1_12rem] mr-0 px-1.5 py-1 text-xs"
          value={add}
          disabled={busy}
          placeholder="an address that is theirs, e.g. ada@loomworks.example"
          aria-label={`Identity to add to ${p.displayName}`}
          onChange={(e) => setAdd(e.target.value)}
        />
        <Button
          type="submit"
          variant="subtle"
          density="compact"
          disabled={busy || asIdentity(add) === ""}
        >
          attach
        </Button>
      </form>
    </article>
  );
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
      <p className="my-1.5 mb-2 text-[.74rem] text-muted">
        Everyone the corpus knows, and every address that resolves to them. The mail client reads
        its senders from here, so correcting a name or an address here corrects it wherever that
        person appears — in the list, in a thread, in the person field of the search. Two rows that
        are one human are one row after a merge, and that is the plan below, where the evidence is.
      </p>
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      {last ? (
        <p className="my-1.5 mb-2 text-[.74rem] text-muted">
          {last}. The list below is the current one.
        </p>
      ) : null}
      <FormField
        className="my-2 mb-1 flex items-center gap-1.5 text-[.78rem]"
        label="Find a person"
      >
        <TextInput
          className="min-w-0 flex-[0_1_18rem] mr-0 px-1.5 py-1 text-xs"
          value={q}
          placeholder="a name or an address"
          aria-label="Find a person"
          onChange={(e) => setQ(e.target.value)}
        />
      </FormField>
      {!people.data ? (
        <p className="my-1.5 mb-2 text-[.74rem] text-muted">
          {people.isPending ? "Reading the people…" : "No people."}
        </p>
      ) : shown.hits.length === 0 ? (
        <p className="my-1.5 mb-2 text-[.74rem] text-muted">
          Nobody matches {q.trim() === "" ? "—" : <code>{q.trim()}</code>}. A person the corpus has
          never seen cannot be added here: a row exists because the mail named them, and a hand-made
          row would be one no message could ever reach.
        </p>
      ) : (
        <ol className="mt-2 list-none p-0">
          {shown.capped.map((p) => (
            <li
              key={p.personId}
              className="not-first:mt-2 rounded-[9px] border border-line bg-card px-3 py-2"
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
        <p className="my-1.5 mb-2 text-[.74rem] text-muted">
          {shown.more} more {shown.more === 1 ? "person" : "people"} match
          {q.trim() === "" ? "" : " that"} — keep typing to narrow it down.
        </p>
      ) : null}
    </>
  );
}
