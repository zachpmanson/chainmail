import { useState } from "react";
import { Autocomplete } from "@base-ui/react/autocomplete";
import type { PersonSummary } from "../../lib/api/api";

/** The API resolves any alias to the whole person (`corpus.personSetSQL`). */
export function queryValue(p: PersonSummary): string {
  const ids = p.identities ?? [];
  const email = ids.find((i) => i.startsWith("email:"));
  if (email) return email.slice("email:".length);
  const uid = ids.find((i) => i.startsWith("slack_uid:"));
  if (uid) return uid.slice("slack_uid:".length);
  return p.displayName;
}

/** Strip prefixes first: matching raw `email:…` identities makes "mai" match everyone. */
function haystack(p: PersonSummary): string {
  const aliases = (p.identities ?? []).map((i) => i.slice(i.indexOf(":") + 1));
  return [p.displayName, ...aliases].join(" ").toLowerCase();
}

const ANYONE = { value: "", name: "Anyone", address: "" };

interface Row {
  value: string;
  name: string;
  address: string;
}

function rows(people: PersonSummary[], typed: string): Row[] {
  const q = typed.trim().toLowerCase();
  const found = people
    .filter((p) => q === "" || haystack(p).includes(q))
    .slice(0, 8)
    .map((p) => ({ value: queryValue(p), name: p.displayName, address: queryValue(p) }));
  return q === "" ? [ANYONE, ...found] : found;
}

/** The list is portalled to the body because the nav's ancestors clip their overflow. */
export default function NavPerson({
  people,
  value,
  onEdit,
  onCommit,
}: {
  people: PersonSummary[];
  value: string;
  onEdit: (text: string) => void;
  onCommit: (person: string) => void;
}) {
  const [open, setOpen] = useState(false);

  return (
    <Autocomplete.Root
      items={rows(people, value)}
      filter={null}
      itemToStringValue={(row) => row.value}
      value={value}
      onValueChange={(text, details) => {
        if (details.reason === "escape-key") {
          // Closed-list Escape belongs to NavSearch, not just the person field.
          details.cancel();
          details.allowPropagation();
        } else if (details.reason === "item-press") {
          onCommit(text);
        } else if (details.reason === "input-change") {
          onEdit(text);
          setOpen(true);
        }
      }}
      open={open}
      onOpenChange={setOpen}
      openOnInputClick
      autoHighlight="always"
      keepHighlight
      submitOnItemClick={false}
    >
      <Autocomplete.Input
        className="min-h-0 max-w-[20ch] rounded-md border border-line bg-bg px-1.5 py-0.5 text-xs text-fg"
        autoComplete="off"
        spellCheck={false}
        onFocus={() => setOpen(true)}
        onKeyDown={(ev) => {
          // First Escape only closes suggestions, even when there are no matches.
          if (ev.key === "Escape" && open) ev.stopPropagation();
        }}
      />
      <Autocomplete.Portal>
        <Autocomplete.Positioner
          className="z-60 w-[max(var(--anchor-width),15rem)] data-empty:hidden"
          sideOffset={4}
          align="start"
          positionMethod="fixed"
        >
          <Autocomplete.Popup className="rounded-md border border-line bg-card shadow-[0_8px_24px_rgba(0,0,0,.22)]">
            <Autocomplete.List
              className="m-0 max-h-60 overflow-x-hidden overflow-y-auto p-1"
              aria-label="Who the search is narrowed to"
            >
              {(row: Row) => (
                <Autocomplete.Item
                  key={row.value || "anyone"}
                  value={row}
                  className="flex cursor-pointer flex-col gap-px rounded-md px-2 py-1 data-highlighted:bg-mine"
                >
                  <span className="text-xs wrap-anywhere">{row.name}</span>
                  {row.address && row.address !== row.name ? (
                    <span className="text-2xs wrap-anywhere text-muted">{row.address}</span>
                  ) : null}
                </Autocomplete.Item>
              )}
            </Autocomplete.List>
          </Autocomplete.Popup>
        </Autocomplete.Positioner>
      </Autocomplete.Portal>
    </Autocomplete.Root>
  );
}
