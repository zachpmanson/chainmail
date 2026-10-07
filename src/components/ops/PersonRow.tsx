import { useState } from "react";
import type { PersonSummary } from "../../lib/api/api";
import Button from "../ui/Button";
import TextInput from "../ui/TextInput";
import Identity from "./Identity";

function asIdentity(typed: string): string {
  const t = typed.trim();
  if (t === "") return "";
  return t.includes(":") ? t : `email:${t}`;
}

/** The server refuses an identity another person already has: taking it is a
 *  merge, which belongs on the merge plan. */
export default function PersonRow({
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
      <p className="mt-0 mb-1.5 text-sm/snug">
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
      <p className="my-0.5 text-sm/snug [&_code]:text-xs [&_code]:wrap-anywhere">
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
