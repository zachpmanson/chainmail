import { useEffect, useState } from "react";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { MagnifyingGlassIcon, XMarkIcon } from "@heroicons/react/24/outline";
import { $api, type SearchMode } from "../../lib/api/api";
import NavPerson from "./NavPerson";
import Button from "../ui/Button";
import IconButton from "../ui/IconButton";
import SelectInput from "../ui/SelectInput";
import TextInput from "../ui/TextInput";

const MODES: SearchMode[] = ["hybrid", "semantic", "lexical"];

const isMode = (m: unknown): m is SearchMode =>
  m === "lexical" || m === "semantic" || m === "hybrid";

/** Committed together on submit or blur, not per keystroke. */
interface Asked {
  q: string;
  mode: SearchMode;
  person: string;
  since: string;
  accountId: string;
}

const NOTHING: Asked = { q: "", mode: "hybrid", person: "", since: "", accountId: "" };

/** Mode alone doesn't count; matches Home's rule in router.tsx. */
const asks = (a: Asked) =>
  a.q.trim() !== "" || a.person.trim() !== "" || a.since.trim() !== "" || a.accountId !== "";

const trim = (a: Asked): Asked => ({
  q: a.q.trim(),
  mode: a.mode,
  person: a.person.trim(),
  since: a.since.trim(),
  accountId: a.accountId,
});

/** The same four questions, said two ways. */
const same = (a: Asked, b: Asked) =>
  a.q.trim() === b.q.trim() &&
  a.mode === b.mode &&
  a.person.trim() === b.person.trim() &&
  a.since.trim() === b.since.trim() &&
  a.accountId === b.accountId;

/**
 * Nav search: focus expands it over the nav, blur commits the draft, and Escape, × or clearing
 * the box drops the question but keeps the open thread.
 */
export default function NavSearch() {
  const navigate = useNavigate();
  // Read from the URL each render so links, Back and reload move the box too.
  const asked = useRouterState({
    select: (s): Asked => {
      if (s.location.pathname !== "/") return NOTHING;
      const search = s.location.search as Record<string, unknown>;
      return {
        q: typeof search.q === "string" ? search.q : "",
        mode: isMode(search.mode) ? search.mode : "hybrid",
        person: typeof search.person === "string" ? search.person : "",
        since: typeof search.since === "string" ? search.since : "",
        accountId: typeof search.accountId === "string" ? search.accountId : "",
      };
    },
  });

  const openThread = useRouterState({
    select: (s): string | undefined => {
      if (s.location.pathname !== "/") return undefined;
      const search = s.location.search as Record<string, unknown>;
      return typeof search.open === "string" ? search.open : undefined;
    },
  });

  // The edit in progress, or null when the fields are reading the address.
  const [draft, setDraft] = useState<Asked | null>(null);
  const [open, setOpen] = useState(false);
  const shown = draft ?? asked;

  // Drop the draft only once the new URL lands, or the fields flash the old question for a frame.
  const address = `${asked.q}\u0000${asked.mode}\u0000${asked.person}\u0000${asked.since}\u0000${asked.accountId}`;
  useEffect(() => {
    setDraft(null);
  }, [address]);

  // Only fetched while the panel is open.
  const people = $api.useQuery("get", "/v1/people", {}, { enabled: open });
  const accounts = $api.useQuery("get", "/auth/status", {}, { enabled: open });

  const edit = (patch: Partial<Asked>) => setDraft({ ...shown, ...patch });

  /** No-op when unchanged: rewriting the URL would drop the open folder and thread. */
  const commit = (next: Asked) => {
    const four = trim(next);
    if (same(four, asked)) {
      setDraft(null);
      return;
    }
    navigate({
      to: "/",
      search: {
        // Omit defaults so the default search stays plain /.
        ...(four.q ? { q: four.q } : {}),
        ...(four.mode !== "hybrid" ? { mode: four.mode } : {}),
        ...(four.person ? { person: four.person } : {}),
        ...(four.since ? { since: four.since } : {}),
        ...(four.accountId ? { accountId: four.accountId } : {}),
        // Keep the open thread only when leaving search; a new question starts with an empty pane.
        ...(asks(four) || openThread === undefined ? {} : { open: openThread }),
      },
      // Replace, so Back from a built page lands straight on the search.
      replace: true,
    });
  };

  /**
   * Set the draft to NOTHING rather than dropping it: the × unmounts with the panel, and the
   * browser's late blur would otherwise commit the old question from the URL, undoing the press.
   */
  const dismiss = () => {
    setDraft(NOTHING);
    setOpen(false);
    commit(NOTHING);
  };

  return (
    <>
      {/* Mouse-down is prevented so the field doesn't blur first: blur commits the search and shuts
         the panel, taking this button with it before the click lands. */}
      {open ? (
        <IconButton
          className="navcancel"
          title="Clear the search and shut the panel — Escape"
          aria-label="Clear the search"
          onMouseDown={(ev) => ev.preventDefault()}
          onClick={dismiss}
        >
          <XMarkIcon aria-hidden="true" />
        </IconButton>
      ) : null}
      <form
        className={`navsearch absolute top-0 right-0 flex h-9 w-48 items-center gap-1.5 overflow-hidden rounded-md border border-line bg-card px-2 py-1 [--searchgap:calc(1.85rem+1rem)] focus-within:border-accent motion-safe:transition-[width] motion-safe:duration-180 motion-safe:ease-[ease] [&:hover]:border-accent${open ? " open" : ""}`}
        role="search"
        onSubmit={(ev) => {
          ev.preventDefault();
          commit(shown);
        }}
        onFocus={() => setOpen(true)}
        // Escape shuts the panel without moving focus, so a click must reopen it.
        onClick={() => setOpen(true)}
        // Escape gives the whole search up: see dismiss, which is the same act.
        onKeyDown={(ev) => {
          if (ev.key !== "Escape") return;
          dismiss();
        }}
        onBlur={(ev) => {
          if (ev.currentTarget.contains(ev.relatedTarget as Node | null)) return;
          commit(shown);
          if (!asks(shown)) setOpen(false);
        }}
      >
        <MagnifyingGlassIcon
          className="flex-none opacity-80"
          width={12}
          height={12}
          aria-hidden="true"
        />
        <TextInput
          className="min-h-0 min-w-24 flex-1 border-0 bg-transparent px-0 py-0.5 text-sm text-fg placeholder:text-muted focus:ring-0"
          value={shown.q}
          onChange={(ev) => {
            edit({ q: ev.target.value });
            setOpen(true);
          }}
          aria-label="Search the corpus"
          aria-current={asks(asked) ? "page" : undefined}
          placeholder="Search…"
          autoComplete="off"
          spellCheck={false}
        />
        {open ? (
          <span className="flex min-w-0 flex-[0_1_auto] flex-nowrap items-center gap-2 overflow-x-auto overflow-y-hidden">
            <label className="flex flex-none items-center gap-1">
              <span className="text-2xs font-bold uppercase tracking-[.08em] text-muted">Mode</span>
              <SelectInput
                className="min-h-0 rounded-md border border-line bg-bg px-1.5 py-0.5 text-xs text-fg"
                value={shown.mode}
                onChange={(ev) => {
                  const mode = ev.target.value as SearchMode;
                  edit({ mode });
                  commit({ ...shown, mode });
                }}
              >
                {MODES.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </SelectInput>
            </label>
            <label className="flex flex-none items-center gap-1">
              <span className="text-2xs font-bold uppercase tracking-[.08em] text-muted">
                Person
              </span>
              <NavPerson
                people={people.data?.people ?? []}
                value={shown.person}
                onEdit={(person) => edit({ person })}
                onCommit={(person) => {
                  edit({ person });
                  commit({ ...shown, person });
                }}
              />
            </label>
            <label className="flex flex-none items-center gap-1">
              <span className="text-2xs font-bold uppercase tracking-[.08em] text-muted">
                Gmail account
              </span>
              <SelectInput
                className="min-h-0 rounded-md border border-line bg-bg px-1.5 py-0.5 text-xs text-fg"
                aria-label="Gmail account"
                value={shown.accountId}
                onChange={(ev) => {
                  const accountId = ev.target.value;
                  edit({ accountId });
                  commit({ ...shown, accountId });
                }}
              >
                <option value="">All accounts</option>
                {(accounts.data?.accounts ?? [])
                  .filter((account) => account.signedIn)
                  .map((account) => (
                    <option key={account.id} value={account.id}>
                      {account.email || account.displayName}
                    </option>
                  ))}
              </SelectInput>
            </label>
            <label className="flex flex-none items-center gap-1">
              <span className="text-2xs font-bold uppercase tracking-[.08em] text-muted">
                Since
              </span>
              <TextInput
                className="w-[8.6rem] min-h-0 rounded-md border border-line bg-bg px-1.5 py-0.5 text-xs text-fg"
                type="date"
                value={shown.since}
                onChange={(ev) => {
                  const since = ev.target.value;
                  edit({ since });
                  commit({ ...shown, since });
                }}
              />
            </label>
            {/* Enter submits a form only by way of its submit button. */}
            <Button
              type="submit"
              variant="subtle"
              className="min-h-0 rounded-md border-line bg-mine px-2 py-1 text-xs font-semibold text-fg disabled:cursor-default disabled:opacity-45"
              disabled={!asks(shown)}
            >
              Search
            </Button>
          </span>
        ) : null}
      </form>
    </>
  );
}
