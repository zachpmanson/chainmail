import { useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
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
  const listId = useId();
  const field = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [at, setAt] = useState<{ left: number; top: number; width: number } | null>(null);

  const found = rows(people, value);

  // Re-measured on scroll: the field is fixed but the page under it moves.
  useLayoutEffect(() => {
    if (!open) {
      setAt(null);
      return;
    }
    const place = () => {
      const box = field.current?.getBoundingClientRect();
      if (!box) return;
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
        className="min-h-0 max-w-[20ch] rounded-md border border-line bg-bg px-1.5 py-0.5 text-xs text-fg"
        value={value}
        role="combobox"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        aria-autocomplete="list"
        aria-activedescendant={open && found[active] ? `${listId}-${active}` : undefined}
        autoComplete="off"
        spellCheck={false}
        onChange={(ev) => {
          onEdit(ev.target.value);
          setOpen(true);
          setActive(0);
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onKeyDown={(ev) => {
          if (ev.key === "Escape") {
            // With the list open, Escape only closes it; otherwise NavSearch clears the box.
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
              className="fixed z-60 m-0 max-h-60 list-none overflow-y-auto overflow-x-hidden rounded-md border border-line bg-card p-1 shadow-[0_8px_24px_rgba(0,0,0,.22)]"
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
                  className={`flex flex-col gap-px rounded-md py-1 px-2 cursor-pointer${i === active ? " bg-mine" : ""}`}
                  onMouseDown={(ev) => ev.preventDefault()}
                  onMouseEnter={() => setActive(i)}
                  onClick={() => pick(row.value)}
                >
                  <span className="text-xs wrap-anywhere">{row.name}</span>
                  {row.address && row.address !== row.name ? (
                    <span className="text-2xs text-muted wrap-anywhere">{row.address}</span>
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
