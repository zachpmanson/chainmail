import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { $api, type PersonSummary } from "../lib/api";
import { FormField } from "./FormField";
import { Button, IconButton, TextInput } from "./controls";
import { InlineAlert } from "./InlineAlert";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/** How many people the screen draws at once. A real corpus holds hundreds — this
 *  one holds 431 — so the list is filtered first and capped second, and the note
 *  under it says how many are not drawn rather than letting the screen imply the
 *  corpus is this small. */
const SHOWN = 25;

/** Everything a filter can match a person on: the name they are called, and
 *  every spelling the corpus read them as. An address is the thing a reader has
 *  when they are looking for someone ("who is ben@…"), so searching by one has
 *  to work — the same rule the search field in the nav follows. */
function haystack(p: PersonSummary): string {
  return [p.displayName, ...(p.identities ?? [])].join(" ").toLowerCase();
}

/** What a typed identity means. A bare address is the common case — a reader
 *  with an address in hand should not have to spell its kind — and anything with
 *  a `kind:` prefix is taken as written, so the field can express what the row
 *  shows without a separate control for the kind. */
function asIdentity(typed: string): string {
  const t = typed.trim();
  if (t === "") return "";
  return t.includes(":") ? t : `email:${t}`;
}

/** One identity of one person: what the corpus resolves, and the detach. The ×
 *  writes immediately, because detaching is one act — it has no counterpart that
 *  has to be saved with it. */
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
    <span className="mr-[.3rem] mb-[.2rem] inline-flex items-center gap-[.15rem] rounded-[5px] border border-line bg-card px-[.3rem] py-[.05rem] pr-[.2rem]">
      <code className="text-[.72rem] [overflow-wrap:anywhere]">{id}</code>
      <IconButton
        type="button"
        className="!size-auto !min-h-0 rounded px-[.18rem] py-0 text-[.85rem] leading-none text-muted hover:bg-quote hover:text-red-700 disabled:opacity-40"
        disabled={busy}
        title={`detach ${id}`}
        aria-label={`Detach ${id} from ${owner.displayName}`}
        onClick={() => onDetach(id)}
      >
        ×
      </IconButton>
    </span>
  );
}

/** One person: what the corpus says about them, and the edits.
 *
 *  Three acts, each one write, because each is one decision: rename (what to
 *  call them), attach an identity (an address or uid that is theirs), detach one
 *  (it is not). The corpus keeps the spellings it read from headers whatever the
 *  name says — a name is evidence — and it refuses an identity another person
 *  already answers to, because taking one is a merge and a merge is made on the
 *  plan below, where the evidence is. */
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
    <article className="opmerge">
      {/* Not .opmrule: that line is the plan's rule label and is shouted in
          capitals, which is right for "same display name" and wrong for a
          person's own name. */}
      <p className="mt-0 mb-[.35rem] text-[.86rem] leading-[1.3]">
        <span className="text-muted">#{p.personId}</span>{" "}
        <span className="ml-[.15rem] font-semibold">{p.displayName}</span>{" "}
        <span className="text-muted">{mail}</span>
      </p>
      <form
        className="opwhoedit my-[.2rem] mb-[.1rem] flex flex-wrap items-center gap-[.35rem]"
        onSubmit={(e) => {
          e.preventDefault();
          if (!renamed) return;
          onEdit({ displayName: wanted.trim() });
          setName(null);
        }}
      >
        <TextInput
          className="min-w-0 flex-[0_1_16rem] mr-0 px-[.4rem] py-[.2rem] text-xs"
          value={wanted}
          disabled={busy}
          aria-label={`Name for ${p.displayName}`}
          onChange={(e) => setName(e.target.value)}
        />
        <Button
          type="submit"
          variant="subtle"
          density="compact"
          className="opbtn"
          disabled={busy || !renamed}
        >
          rename
        </Button>
      </form>
      <p className="my-[.15rem] text-[.84rem] leading-[1.35] [&_code]:text-[.74rem] [&_code]:[overflow-wrap:anywhere]">
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
        className="oporgadd mt-[.6rem] flex flex-wrap items-center gap-[.35rem] border-t border-[var(--line)] pt-[.6rem]"
        onSubmit={(e) => {
          e.preventDefault();
          const id = asIdentity(add);
          if (id === "") return;
          onEdit({ addIdentities: [id] });
          setAdd("");
        }}
      >
        <TextInput
          className="min-w-0 flex-[0_1_12rem] mr-0 px-[.4rem] py-[.2rem] text-xs"
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
          className="opbtn"
          disabled={busy || asIdentity(add) === ""}
        >
          attach
        </Button>
      </form>
    </article>
  );
}

/** The people the corpus knows, and the edits a reader makes to one.
 *
 *  This is the whole of the people setup: who exists, what they are called, and
 *  which addresses resolve to them. It is the surface the mail client reads from
 *  — the reader's own person is one of these rows (marked below by the settings
 *  screen), and the search field's person picker is this list filtered by name.
 *
 *  Merging is deliberately not here. A merge is a claim that two rows are one
 *  human, and it is the one act on this screen that cannot be undone, so it
 *  lives on the plan above with the evidence that supports it. */
export function OpsPeople() {
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
      // A refusal is the corpus's answer, and for the interesting one — an
      // address that belongs to somebody else — it is the instruction for what
      // to do instead. The list is refetched either way.
      setError(`${name}: ${errText(e)}`);
    }
    setBusy(false);
    await qc.invalidateQueries({ queryKey: ["get", "/v1/people"] });
    // Identity changes move the merge plan: two people who now share nothing may
    // have been a same-name pair, and vice versa.
    await qc.invalidateQueries({ queryKey: ["get", "/v1/ops/plan"] });
  }

  return (
    <>
      <p className="my-[.35rem] mb-2 text-[.74rem] text-[var(--muted)]">
        Everyone the corpus knows, and every address that resolves to them. The mail client reads
        its senders from here, so correcting a name or an address here corrects it wherever that
        person appears — in the list, in a thread, in the person field of the search. Two rows that
        are one human are one row after a merge, and that is the plan below, where the evidence is.
      </p>
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      {last ? (
        <p className="my-[.35rem] mb-2 text-[.74rem] text-[var(--muted)]">
          {last}. The list below is the current one.
        </p>
      ) : null}
      <FormField
        className="opfilter my-[.5rem] mb-[.2rem] flex items-center gap-[.4rem] text-[.78rem]"
        label="Find a person"
      >
        <TextInput
          className="min-w-0 flex-[0_1_18rem] mr-0 px-[.4rem] py-[.2rem] text-xs"
          value={q}
          placeholder="a name or an address"
          aria-label="Find a person"
          onChange={(e) => setQ(e.target.value)}
        />
      </FormField>
      {!people.data ? (
        <p className="my-[.35rem] mb-2 text-[.74rem] text-[var(--muted)]">
          {people.isPending ? "Reading the people…" : "No people."}
        </p>
      ) : shown.hits.length === 0 ? (
        <p className="my-[.35rem] mb-2 text-[.74rem] text-[var(--muted)]">
          Nobody matches {q.trim() === "" ? "—" : <code>{q.trim()}</code>}. A person the corpus has
          never seen cannot be added here: a row exists because the mail named them, and a hand-made
          row would be one no message could ever reach.
        </p>
      ) : (
        <ol className="mt-2 list-none p-0">
          {shown.capped.map((p) => (
            <li
              key={p.personId}
              className="oprow not-first:mt-[.45rem] rounded-[9px] border border-[var(--line)] bg-[var(--card)] px-[.7rem] py-[.55rem]"
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
        <p className="my-[.35rem] mb-2 text-[.74rem] text-[var(--muted)]">
          {shown.more} more {shown.more === 1 ? "person" : "people"} match
          {q.trim() === "" ? "" : " that"} — keep typing to narrow it down.
        </p>
      ) : null}
    </>
  );
}
