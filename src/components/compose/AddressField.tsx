import { Autocomplete } from "@base-ui/react/autocomplete";
import { XMarkIcon } from "@heroicons/react/24/outline";
import { useMemo, useRef, useState } from "react";
import Button from "../ui/Button";

/** Text that matches no suggestion still becomes a chip once it looks like an
 *  address; real validation is the server's (see cmd/server's checkAddresses). */

export type Address = { name?: string; address: string };

/** Always includes the address: this is the last screen before an unrecallable send. */
export function addressWords(a: Address): string {
  return a.name ? `${a.name} <${a.address}>` : a.address;
}

export const addressKey = (address: string) => address.trim().toLowerCase();

/** Deliberately shallow; accepts `Name <addr>` so a pasted name needn't be stripped. */
export function typedAddress(text: string): Address | null {
  const trimmed = text.trim();
  const angled = /^(.*?)\s*<([^<>]+)>$/.exec(trimmed);
  const bare = (angled?.[2] ?? trimmed).trim();
  if (!/^[^\s@,;<>]+@[^\s@,;<>]+$/.test(bare)) return null;
  const name = angled?.[1]?.trim().replace(/^"(.*)"$/, "$1") ?? "";
  return name ? { name, address: bare } : { address: bare };
}

function matches(suggestion: Address, query: string): boolean {
  if (query === "") return true;
  const words = query.toLowerCase();
  return (
    suggestion.address.toLowerCase().includes(words) ||
    (suggestion.name ?? "").toLowerCase().includes(words)
  );
}

function Chip({ who, remove, disabled }: { who: Address; remove: () => void; disabled: boolean }) {
  const words = addressWords(who);
  return (
    <span
      className="inline-flex max-w-full items-center gap-1 rounded-lg border border-line bg-card px-1.5 py-px text-xs"
      title={words}
    >
      <span className="wrap-anywhere">{words}</span>
      <Button
        type="button"
        variant="bare"
        className="inline-flex shrink-0 items-center justify-center rounded-sm border border-transparent p-0.5 text-muted transition-colors hover:border-line hover:bg-bg hover:text-accent disabled:cursor-default disabled:opacity-55 disabled:hover:bg-transparent disabled:hover:text-muted"
        disabled={disabled}
        onClick={remove}
        title={`Take ${who.address} off this reply.`}
        aria-label={`remove ${words}`}
      >
        <XMarkIcon className="block" width={10} height={10} aria-hidden="true" />
      </Button>
    </span>
  );
}

export default function AddressField({
  label,
  value,
  onChange,
  suggestions,
  taken = [],
  mine = [],
  disabled = false,
}: {
  /** "to" or "cc"; the accessible name of the input and every chip. */
  label: string;
  /** The addresses this list holds, in the order the reader put them. */
  value: Address[];
  onChange: (next: Address[]) => void;
  suggestions: Address[];
  /** Addresses in the other list; a recipient can be in only one. */
  taken?: Address[];
  /** The reader's own addresses, which a reply is not sent to. */
  mine?: string[];
  disabled?: boolean;
}) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const highlighted = useRef<Address | undefined>(undefined);
  const [refused, setRefused] = useState<string | null>(null);
  const field = useRef<HTMLSpanElement | null>(null);

  const here = useMemo(() => new Set(value.map((a) => addressKey(a.address))), [value]);
  const elsewhere = useMemo(() => new Set(taken.map((a) => addressKey(a.address))), [taken]);
  const own = useMemo(() => new Set(mine.map(addressKey)), [mine]);

  const words = query.trim();
  const offered = useMemo(
    () =>
      suggestions.filter((a) => {
        const key = addressKey(a.address);
        return !here.has(key) && !elsewhere.has(key) && !own.has(key);
      }),
    [suggestions, here, elsewhere, own],
  );
  const matches_ = useMemo(
    () => offered.filter((a) => matches(a, words)).slice(0, 8),
    [offered, words],
  );
  const typed = typedAddress(words);
  const typedRow =
    typed && !matches_.some((a) => addressKey(a.address) === addressKey(typed.address))
      ? typed
      : null;
  const rows = typedRow ? [...matches_, typedRow] : matches_;

  const showing = open && words !== "";

  function add(who: Address) {
    if (disabled) return;
    const key = addressKey(who.address);
    if (own.has(key)) {
      setRefused(
        `${who.address} is your own address — a reply is not sent to the person writing it.`,
      );
      return;
    }
    if (here.has(key)) {
      setRefused(`${who.address} is already on this reply.`);
      return;
    }
    if (elsewhere.has(key)) {
      setRefused(
        `${who.address} is already on the reply, in the other list. Take it out of there to move it.`,
      );
      return;
    }
    onChange([...value, who]);
    setQuery("");
    setRefused(null);
    setOpen(false);
    highlighted.current = undefined;
  }

  return (
    <Autocomplete.Root
      items={rows}
      filter={null}
      itemToStringValue={addressWords}
      value={query}
      onValueChange={(text, details) => {
        if (details.reason === "item-press") {
          const who = rows.find((a) => addressWords(a) === text);
          if (who) add(who);
        } else if (details.reason === "input-change") {
          setQuery(text);
          setRefused(null);
          setOpen(true);
        }
      }}
      disabled={disabled}
      open={showing && !disabled}
      onOpenChange={setOpen}
      onItemHighlighted={(who) => {
        highlighted.current = who;
      }}
      autoHighlight="always"
      keepHighlight
      openOnInputClick
      submitOnItemClick={false}
    >
      <span
        ref={field}
        className="relative mx-0.5 inline-flex min-h-7 w-full max-w-full min-w-0 flex-wrap items-center gap-1 rounded-md border border-line bg-bg px-1 py-0.5 align-middle focus-within:border-accent"
        data-list={label}
      >
        {value.map((who) => (
          <Chip
            key={addressKey(who.address)}
            who={who}
            disabled={disabled}
            remove={() =>
              onChange(value.filter((a) => addressKey(a.address) !== addressKey(who.address)))
            }
          />
        ))}
        <Autocomplete.Input
          className="min-w-12 flex-[1_1_7rem] border-0 bg-transparent px-0 py-0.5 text-xs text-fg outline-none placeholder:text-muted"
          type="text"
          aria-label={`${label} addresses`}
          autoComplete="off"
          placeholder={value.length ? "" : `add an address`}
          onFocus={() => setOpen(true)}
          onKeyDown={(e) => {
            if (disabled) return;
            if (e.nativeEvent.isComposing || e.which === 229) {
              e.preventBaseUIHandler();
              return;
            }
            if (e.key === "Enter" || e.key === "," || e.key === ";") {
              // Recipient acceptance never submits the enclosing compose form.
              e.preventDefault();
              e.preventBaseUIHandler();
              const pick = (showing ? highlighted.current : undefined) ?? typed;
              if (pick) add(pick);
            } else if (e.key === "Escape" && (query !== "" || refused || showing)) {
              e.preventDefault();
              e.preventBaseUIHandler();
              e.stopPropagation();
              setQuery("");
              setRefused(null);
              setOpen(false);
            } else if (e.key === "Backspace" && query === "" && value.length) {
              e.preventDefault();
              e.preventBaseUIHandler();
              onChange(value.slice(0, -1));
            }
          }}
        />
        <Autocomplete.Portal>
          <Autocomplete.Positioner
            anchor={field}
            className="z-60 w-(--anchor-width) max-w-96"
            sideOffset={4}
            align="start"
            positionMethod="fixed"
          >
            <Autocomplete.Popup className="rounded-md border border-line bg-card p-0.5 shadow-[0_6px_18px_rgba(0,0,0,.18)]">
              <Autocomplete.List
                className="m-0 flex max-h-56 flex-col overflow-auto p-0"
                aria-label={`${label} suggestions`}
              >
                {(a: Address) => (
                  <Autocomplete.Item
                    key={addressKey(a.address)}
                    value={a}
                    className="flex cursor-pointer items-baseline gap-1.5 rounded-sm px-1.5 py-1 text-xs data-highlighted:bg-mine"
                  >
                    <span className="wrap-anywhere">{a.name ?? a.address}</span>
                    {a.name ? (
                      <span className="text-2xs wrap-anywhere text-muted">{a.address}</span>
                    ) : null}
                  </Autocomplete.Item>
                )}
              </Autocomplete.List>
              {rows.length === 0 ? (
                <span className="cursor-default text-xs text-muted" role="status">
                  {typed === null && words !== ""
                    ? `${words} is not an address — an address has something@somewhere in it.`
                    : "No address matches."}
                </span>
              ) : null}
            </Autocomplete.Popup>
          </Autocomplete.Positioner>
        </Autocomplete.Portal>
        {refused ? (
          <span className="basis-full px-1 py-0.5 text-2xs text-red-700" role="status">
            {refused}
          </span>
        ) : null}
      </span>
    </Autocomplete.Root>
  );
}
