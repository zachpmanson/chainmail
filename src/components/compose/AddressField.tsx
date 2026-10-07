import { XMarkIcon } from "@heroicons/react/24/outline";
import { useMemo, useRef, useState } from "react";
import { Button } from "../ui/controls";

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
        variant="quiet"
        className="size-auto! min-h-0! shrink-0 rounded p-0.5 text-muted hover:bg-bg hover:text-accent disabled:cursor-default disabled:opacity-[.55] disabled:hover:bg-transparent disabled:hover:text-muted"
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
  const [active, setActive] = useState(0);
  const [refused, setRefused] = useState<string | null>(null);
  const input = useRef<HTMLInputElement | null>(null);

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
    setActive(0);
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "ArrowDown" && rows.length) {
      e.preventDefault();
      setOpen(true);
      setActive((i) => Math.min(i + 1, rows.length - 1));
      return;
    }
    if (e.key === "ArrowUp" && rows.length) {
      e.preventDefault();
      setActive((i) => Math.max(i - 1, 0));
      return;
    }
    if (e.key === "Enter" || e.key === "," || e.key === ";") {
      const pick = rows[active] ?? typed;
      if (!pick) return;
      e.preventDefault();
      add(pick);
      return;
    }
    if (e.key === "Escape" && (words !== "" || showing)) {
      e.preventDefault();
      setQuery("");
      setRefused(null);
      setOpen(false);
      return;
    }
    if (e.key === "Backspace" && query === "" && value.length) {
      e.preventDefault();
      onChange(value.slice(0, -1));
    }
  }

  return (
    <span
      className={`relative inline-flex w-full min-w-0 min-h-7 max-w-full flex-wrap items-center gap-1 align-middle rounded-md border border-line bg-bg px-1 py-0.5 mx-0.5 focus-within:border-accent${showing ? " open" : ""}`}
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
      <input
        ref={input}
        className="min-w-12 flex-[1_1_7rem] border-0 bg-transparent px-0 py-0.5 text-xs text-fg outline-none placeholder:text-muted"
        type="text"
        role="combobox"
        aria-label={`${label} addresses`}
        aria-autocomplete="list"
        aria-expanded={showing}
        aria-controls={`${label}-suggestions`}
        aria-activedescendant={showing && rows[active] ? `${label}-option-${active}` : undefined}
        autoComplete="off"
        placeholder={value.length ? "" : `add an address`}
        value={query}
        disabled={disabled}
        onChange={(e) => {
          setQuery(e.target.value);
          setRefused(null);
          setOpen(true);
          setActive(0);
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onKeyDown={onKeyDown}
      />
      {showing ? (
        <span
          className="absolute top-full left-0 z-20 mt-1 flex max-h-56 min-w-full max-w-96 flex-col overflow-auto rounded-md border border-line bg-card p-0.5 shadow-[0_6px_18px_rgba(0,0,0,.18)]"
          role="listbox"
          id={`${label}-suggestions`}
          aria-label={`${label} suggestions`}
        >
          {rows.map((a, i) => (
            <span
              key={addressKey(a.address)}
              id={`${label}-option-${i}`}
              className={`flex items-baseline gap-1.5 rounded py-1 px-1.5 text-xs cursor-pointer${i === active ? " bg-mine" : ""}`}
              role="option"
              aria-selected={i === active}
              // Keep focus on mousedown, or the blur closes the list before the click lands.
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => add(a)}
              title={a.name ? `${a.name} <${a.address}>` : a.address}
            >
              <span className="wrap-anywhere">{a.name ?? a.address}</span>
              {a.name ? (
                <span className="text-2xs text-muted wrap-anywhere">{a.address}</span>
              ) : null}
            </span>
          ))}
          {rows.length === 0 ? (
            <span className="text-muted cursor-default" aria-disabled="true">
              {typed === null && words !== ""
                ? `${words} is not an address — an address has something@somewhere in it.`
                : "No address matches."}
            </span>
          ) : null}
        </span>
      ) : null}
      {refused ? (
        <span className="basis-full px-1 py-0.5 text-2xs text-red-700" role="status">
          {refused}
        </span>
      ) : null}
    </span>
  );
}
