import { useMemo, useRef, useState } from "react";

/**
 * The address field: one control that builds a list of addresses — the mechanism a
 * reply's To and Cc are edited through, and the only thing on this page a reader
 * types an address into.
 *
 * **The suggestions are the corpus's, not an address book this component keeps.** A
 * caller hands them in (see ReplyBox, which draws them from /v1/people and from the
 * addresses on the message being answered), and what this component does with them
 * is filter them as the reader types and hand back what was picked. It knows nothing
 * about where they came from or what a person is — an address and the name to print
 * beside it, which is all an address chip needs.
 *
 * **Typing an address that matches nothing still works**, and that is the point of
 * the control rather than a fallback: a reader who is answering somebody the corpus
 * has never seen has no suggestion to pick, so what they type becomes a chip once it
 * looks like an address. "Looks like an address" is deliberately shallow (an `@`
 * with something either side of it, in a bare address or inside angle brackets) —
 * whether it can be delivered to is the mailbox's answer, and the last place a shape
 * is checked is the server (see cmd/server's checkAddresses).
 *
 * **The list is the reader's to arrange and nothing may be said twice.** Three
 * addresses are refused however they are typed or picked: one already in this list,
 * one in the other one (a recipient is one recipient, in one list — see
 * gmailclient's ReplyOptions), and one of the reader's own addresses, which a reply
 * is not sent to. The refusal is said in words beside the field rather than by
 * silently dropping the text, because a control that answered a press with nothing
 * reads as broken.
 *
 * The markup is phrasing content throughout — spans, an input, buttons — because
 * ReplyBox embeds each field in a labeled recipient row on the compose screen.
 */

/** One address as this page holds it: the address, which is what a message is sent
 *  to, and the name to print with it when something knows one. Structurally the
 *  contract's Recipient (see api.d.ts) and gmailclient's Recipient. */
export type Address = { name?: string; address: string };

/** An address as a chip prints it: `Ada Okoye <ada@loomworks.example>`, or the bare
 *  address when nothing knew a name for it. The address is always in there — this is
 *  the last screen before a message that cannot be recalled, and the address is what
 *  it is sent to. */
export function addressWords(a: Address): string {
  return a.name ? `${a.name} <${a.address}>` : a.address;
}

/** The fold two addresses are the same by. The domain is case-insensitive and the
 *  local part is in practice, so a chip and a suggestion that differ only in case are
 *  one person rather than two. */
export const addressKey = (address: string) => address.trim().toLowerCase();

/** What a reader typed, as an address — or nothing, when it is not one.
 *
 *  Deliberately shallow: an `@` with something either side, and no separators or
 *  spaces around it. `Ada Okoye <ada@loomworks.example>` is read as that name and
 *  that address, because a reader pasting a name out of a message should not have to
 *  strip it first. Anything past this — whether the domain resolves, whether the
 *  mailbox exists — is the mailbox's answer rather than this component's.
 */
export function typedAddress(text: string): Address | null {
  const trimmed = text.trim();
  const angled = /^(.*?)\s*<([^<>]+)>$/.exec(trimmed);
  const bare = (angled?.[2] ?? trimmed).trim();
  if (!/^[^\s@,;<>]+@[^\s@,;<>]+$/.test(bare)) return null;
  const name = angled?.[1]?.trim().replace(/^"(.*)"$/, "$1") ?? "";
  return name ? { name, address: bare } : { address: bare };
}

/** Whether an address halves as the reader typed them: the part before the `@`, and
 *  the part after. A suggestion matches when the reader's text is in either, so
 *  "loom" finds loomworks.example and "oka" finds Cy Okafor — the two things a
 *  reader half-remembers about a person. */
function matches(suggestion: Address, query: string): boolean {
  if (query === "") return true;
  const words = query.toLowerCase();
  return (
    suggestion.address.toLowerCase().includes(words) ||
    (suggestion.name ?? "").toLowerCase().includes(words)
  );
}

/** One chip: the address, the press that sends it in the other list, and the press
 *  that takes it off. Both presses are named by the address they act on, because
 *  "remove" twelve times over tells a screen reader nothing about which one. */
function Chip({
  who,
  moveTo,
  move,
  remove,
  disabled,
}: {
  who: Address;
  /** The list this chip's move press sends the address to, or null when the caller
   *  has nowhere for it to move — a lone field, and not two lists of one reply. */
  moveTo: string | null;
  move?: (a: Address) => void;
  remove: () => void;
  disabled: boolean;
}) {
  const words = addressWords(who);
  return (
    <span className="addrchip" title={words}>
      <span className="addrname">{words}</span>
      {moveTo && move ? (
        <button
          type="button"
          className="addrmove"
          disabled={disabled}
          onClick={() => move(who)}
          title={`Send this one in ${moveTo} instead of the list it is in.`}
          aria-label={`send ${words} in ${moveTo} instead`}
        >
          {moveTo}
        </button>
      ) : null}
      <button
        type="button"
        className="addrx"
        disabled={disabled}
        onClick={remove}
        title={`Take ${who.address} off this reply.`}
        aria-label={`remove ${words}`}
      >
        {/* The multiplication sign, drawn rather than typed: it is a mark on a
            control, and the label above is what says what it does. */}
        <svg viewBox="0 0 12 12" width="10" height="10" aria-hidden="true">
          <path d="M2.5 2.5 9.5 9.5M9.5 2.5 2.5 9.5" fill="none" stroke="currentColor"
            strokeWidth="1.4" strokeLinecap="round" />
        </svg>
      </button>
    </span>
  );
}

export function AddressField({
  label,
  value,
  onChange,
  suggestions,
  taken = [],
  mine = [],
  moveTo,
  move,
  disabled = false,
}: {
  /** The list this field is, in the words the sentence around it uses: "to" or
   *  "cc". It is the accessible name of the input and of every chip in it. */
  label: string;
  /** The addresses this list holds, in the order the reader put them. */
  value: Address[];
  onChange: (next: Address[]) => void;
  /** Every address the page can offer, in the order it would rather offer them
   *  (see ReplyBox). Filtered by what the reader types and by what is already on
   *  the reply. */
  suggestions: Address[];
  /** The addresses on the reply in the *other* list, which may not be added here:
   *  one recipient is one address in one list. */
  taken?: Address[];
  /** The reader's own addresses, which a reply is not sent to. */
  mine?: string[];
  /** The other list, for the press that moves a chip between them, or absent when
   *  there is nowhere to move to. */
  moveTo?: string;
  move?: (a: Address) => void;
  disabled?: boolean;
}) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  /** What the last press that could not be honoured said. Cleared by the next thing
   *  the reader does, because it is an answer about the address they just typed
   *  rather than a standing fact about the field. */
  const [refused, setRefused] = useState<string | null>(null);
  const input = useRef<HTMLInputElement | null>(null);

  const here = useMemo(() => new Set(value.map((a) => addressKey(a.address))), [value]);
  const elsewhere = useMemo(() => new Set(taken.map((a) => addressKey(a.address))), [taken]);
  const own = useMemo(() => new Set(mine.map(addressKey)), [mine]);

  const words = query.trim();
  // Everything already spoken for, so a reader is never offered what the field
  // would refuse: their own address, one in this list, and one in the other.
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
  // What the reader typed, when it is an address no suggestion already names: the
  // row that makes "an address the corpus has never seen" visible rather than a
  // thing that only works if they press Enter at the right moment.
  const typed = typedAddress(words);
  const typedRow =
    typed && !matches_.some((a) => addressKey(a.address) === addressKey(typed.address))
      ? typed
      : null;
  const rows = typedRow ? [...matches_, typedRow] : matches_;

  const showing = open && words !== "";

  /** Add an address, or say why it cannot be added. The refusals are the three
   *  things that are already spoken for, and they are checked here rather than left
   *  to the server because the reader is owed the answer at the keyboard. */
  function add(who: Address) {
    const key = addressKey(who.address);
    if (own.has(key)) {
      setRefused(`${who.address} is your own address — a reply is not sent to the person writing it.`);
      return;
    }
    if (here.has(key)) {
      setRefused(`${who.address} is already on this reply.`);
      return;
    }
    if (elsewhere.has(key)) {
      setRefused(`${who.address} is already on the reply, in the other list. Take it out of there to move it.`);
      return;
    }
    onChange([...value, who]);
    setQuery("");
    setRefused(null);
    setOpen(false);
    setActive(0);
  }

  /** The press that moves a chip to the other list: the same address, taken out of
   *  this list and handed to the caller to put in the other one. Out of this list
   *  first, so that an address already in the other one is refused by the same rule
   *  that would refuse it here, and this list is left holding it. */
  function hand(to: Address) {
    if (elsewhere.has(addressKey(to.address))) {
      setRefused(`${to.address} is already on the reply, in ${moveTo ?? "the other list"}.`);
      return;
    }
    onChange(value.filter((a) => addressKey(a.address) !== addressKey(to.address)));
    move?.(to);
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
      // Enter takes the row the reader has moved to, or what they typed when they
      // have moved to nothing; a comma or a semicolon is the same act, because a
      // list of addresses separated by commas is how an address field is typed into
      // everywhere else. Nothing to take leaves the text alone rather than guessing.
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
      // The last chip, which is the one the caret is against: a field that builds a
      // list has to be able to unbuild it without reaching for the mouse.
      e.preventDefault();
      onChange(value.slice(0, -1));
    }
  }

  return (
    <span className={`addrfield${showing ? " open" : ""}`} data-list={label}>
      {value.map((who) => (
        <Chip
          key={addressKey(who.address)}
          who={who}
          moveTo={move ? (moveTo ?? null) : null}
          move={hand}
          disabled={disabled}
          remove={() => onChange(value.filter((a) => addressKey(a.address) !== addressKey(who.address)))}
        />
      ))}
      {/* aria-autocomplete, expanded, controls and activedescendant are what make
          this a combobox rather than a text box with a list under it: a screen
          reader is told there is a list, whether it is open, and which row the
          reader is on — which for a sighted reader is the highlight. */}
      <input
        ref={input}
        className="addrinput"
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
        <span className="addrlist" role="listbox" id={`${label}-suggestions`} aria-label={`${label} suggestions`}>
          {rows.map((a, i) => (
            <span
              key={addressKey(a.address)}
              id={`${label}-option-${i}`}
              className={`addropt${i === active ? " on" : ""}`}
              role="option"
              aria-selected={i === active}
              // The press is taken on mousedown, with the input's focus kept: a
              // click that blurred the field first would close the list under the
              // pointer and the press would land on nothing.
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => add(a)}
              title={a.name ? `${a.name} <${a.address}>` : a.address}
            >
              <span className="addroptname">{a.name ?? a.address}</span>
              {a.name ? <span className="addroptaddr">{a.address}</span> : null}
            </span>
          ))}
          {rows.length === 0 ? (
            <span className="addropt none" aria-disabled="true">
              {typed === null && words !== ""
                ? `${words} is not an address — an address has something@somewhere in it.`
                : "No address matches."}
            </span>
          ) : null}
        </span>
      ) : null}
      {refused ? (
        <span className="addrrefuse" role="status">
          {refused}
        </span>
      ) : null}
    </span>
  );
}
