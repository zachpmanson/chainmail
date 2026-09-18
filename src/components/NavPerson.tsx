import { useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { PersonSummary } from "../lib/api";

/**
 * How a person is written into a query: an address, a Slack uid, or the display
 * name when the corpus knows them by nothing else. The API resolves any of the
 * three through the identity graph and answers with the person the string folds
 * into (`corpus.personSetSQL`), so naming one alias of several does not change
 * who is matched — one person, every alias, which is the whole reason a pick
 * writes a value the corpus chose rather than the text that was typed.
 */
export function queryValue(p: PersonSummary): string {
  const ids = p.identities ?? [];
  const email = ids.find((i) => i.startsWith("email:"));
  if (email) return email.slice("email:".length);
  const uid = ids.find((i) => i.startsWith("slack_uid:"));
  if (uid) return uid.slice("slack_uid:".length);
  return p.displayName;
}

/** Everything a person can be looked up by, lowercased once: their name and
 *  every alias the corpus holds. Typing "ada", "byron" or "okoye" all find the
 *  same person — the reader is looking for a person, not for a field of one.
 *
 *  The kind is taken off each alias first. Every one is written `email:…` or
 *  `display_name:…`, and "email" contains "mai": matching the identities as they
 *  are stored makes almost every query match almost every person, which looks
 *  like a filter that works and is a filter that is not there. */
function haystack(p: PersonSummary): string {
  const aliases = (p.identities ?? []).map((i) => i.slice(i.indexOf(":") + 1));
  return [p.displayName, ...aliases].join(" ").toLowerCase();
}

/** The one row that clears the filter, offered first when nothing is typed. A
 *  control that can be narrowed by typing needs a way back to nobody in
 *  particular, and "Anyone" is what the empty value means (see NavSearch). */
const ANYONE = { value: "", name: "Anyone", address: "" };

interface Row {
  value: string;
  name: string;
  address: string;
}

/** The rows the list shows for what has been typed: every person the corpus
 *  knows, narrowed by a substring of their name or any of their aliases.
 *
 *  Everyone is offered, in the corpus's own order — most involved first, which is
 *  where the reader of their own corpus is. A person known only by a name is
 *  still a fact about who was on a thread, so they are offered too; the reader
 *  control on /settings is the one that needs a mailbox, because mail has to have
 *  come from an address.
 *
 *  Deliberately short: this is a suggestion for a field, not a directory, and a
 *  list longer than the field it hangs under is a list the reader scrolls
 *  instead of typing. What is not in it can still be typed — a name the corpus
 *  resolves, or an address it has never seen, both of which the API answers
 *  honestly (see NavSearch for what a filter for nobody means).
 */
function rows(people: PersonSummary[], typed: string): Row[] {
  const q = typed.trim().toLowerCase();
  const found = people
    .filter((p) => q === "" || haystack(p).includes(q))
    .slice(0, 8)
    .map((p) => ({ value: queryValue(p), name: p.displayName, address: queryValue(p) }));
  return q === "" ? [ANYONE, ...found] : found;
}

/**
 * The Person control: a field the reader types in, with the corpus's people
 * under it as they type.
 *
 * A dropdown was the first shape and the wrong one for this: the people the
 * corpus holds number in the hundreds, and a control that offers all of them as
 * options the reader scrolls is a control that has to be read rather than used.
 * Every other narrowed search on the web is a field with suggestions, and the
 * suggestions here can be looked up by name, by address, or by either half of
 * one.
 *
 * What a pick writes is the person's canonical alias rather than the text that
 * found them (see queryValue), because who a search is narrowed to is the
 * corpus's identity graph and not a string to retype. What is *typed* and left
 * alone is committed as itself, which is also right: the same API resolves a
 * display name, and a string that names nobody honestly matches nobody. Neither
 * is decided here — the field reports what was typed and what was picked, and
 * the box's own commit turns that into an address (see NavSearch).
 *
 * The list is drawn into the document body rather than inside the nav, because
 * the nav is three clipped boxes deep — the row scrolls sideways when the
 * controls cannot share it, and each of those ancestors hides its overflow — and
 * a suggestion list inside one of them would be a suggestion list with its
 * bottom cut off. Its place is measured from the field and kept there while the
 * page scrolls under it.
 */
export function NavPerson({
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
  const listId = useId();
  const field = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [at, setAt] = useState<{ left: number; top: number; width: number } | null>(null);

  // What the list offers for what have been typed. The field's value is the
  // question in force, so this reads it rather than keeping a second copy of the
  // text: one record, and it is the address's.
  const found = rows(people, value);

  // Where the list goes, measured from the field: the field is fixed in the
  // header's own row, which does not move, but the page under it does, so the
  // boxes are re-measured on any scroll rather than assumed to stay put.
  useLayoutEffect(() => {
    if (!open) {
      setAt(null);
      return;
    }
    const place = () => {
      const box = field.current?.getBoundingClientRect();
      if (!box) return;
      // At least as wide as the field, and never narrower than an address: the
      // field is capped at 20ch (see .navperson) because it shares its line, and
      // the list is not on that line.
      setAt({ left: box.left, top: box.bottom + 4, width: Math.max(box.width, 240) });
    };
    place();
    window.addEventListener("scroll", place, true);
    window.addEventListener("resize", place);
    return () => {
      window.removeEventListener("scroll", place, true);
      window.removeEventListener("resize", place);
    };
  }, [open]);

  const pick = (value: string) => {
    onCommit(value);
    setOpen(false);
  };

  return (
    <>
      <input
        ref={field}
        className="navperson"
        value={value}
        // A combobox, which is what this is: a text field with a list of
        // suggestions under it. The list is named by the field's own label, so
        // the label text in the panel (see NavSearch) is the whole label.
        role="combobox"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        aria-autocomplete="list"
        aria-activedescendant={open && found[active] ? `${listId}-${active}` : undefined}
        autoComplete="off"
        spellCheck={false}
        onChange={(ev) => {
          // Typing edits and does not ask: a search that ran on the first letter
          // of a name would ask a dozen questions on the way to one, and every
          // one of them would be a page the reader did not ask for.
          onEdit(ev.target.value);
          setOpen(true);
          setActive(0);
        }}
        // The caret arriving is what opens the list, and clicking the field again
        // reopens it: Escape shuts the list without leaving the field.
        onFocus={() => setOpen(true)}
        // Clicking a row must not take the caret out of the field first, or the
        // field's own blur would shut the list out from under the click.
        onBlur={() => setOpen(false)}
        onKeyDown={(ev) => {
          if (ev.key === "Escape") {
            // Escape in a field with a list open closes the list; with the list
            // shut it is the box's Escape, which clears the box (see NavSearch).
            // Stopping it here is what makes those two one gesture with two
            // depths rather than one key that throws away a query.
            if (open) {
              ev.stopPropagation();
              setOpen(false);
            }
            return;
          }
          if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
            if (found.length === 0) return;
            ev.preventDefault();
            setOpen(true);
            const step = ev.key === "ArrowDown" ? 1 : found.length - 1;
            setActive((i) => (i + step) % found.length);
            return;
          }
          if (ev.key === "Enter") {
            // Enter with a row highlighted picks it; with no list it is the
            // form's Enter, which asks the question (see NavSearch).
            const row = open ? found[active] : undefined;
            if (!row) return;
            ev.preventDefault();
            pick(row.value);
          }
        }}
      />
      {open && at && found.length > 0
        ? createPortal(
            <ul
              id={listId}
              className="navpeople"
              role="listbox"
              aria-label="Who the search is narrowed to"
              style={{ left: at.left, top: at.top, width: at.width }}
            >
              {found.map((row, i) => (
                <li
                  key={row.value || "anyone"}
                  id={`${listId}-${i}`}
                  role="option"
                  aria-selected={i === active}
                  className={i === active ? "on" : undefined}
                  onMouseDown={(ev) => ev.preventDefault()}
                  onMouseEnter={() => setActive(i)}
                  onClick={() => pick(row.value)}
                >
                  <span className="who">{row.name}</span>
                  {/* The alias the pick writes, where it differs from the name:
                      the reader sees which of a person's addresses this search
                      will be narrowed to, rather than finding out from the
                      address bar afterwards. */}
                  {row.address && row.address !== row.name ? (
                    <span className="adr">{row.address}</span>
                  ) : null}
                </li>
              ))}
            </ul>,
            document.body,
          )
        : null}
    </>
  );
}
