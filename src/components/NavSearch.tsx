import { useEffect, useState } from "react";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { $api, type SearchMode } from "../lib/api";
import { NavPerson } from "./NavPerson";

/** The default-first order is what the dropdown shows: hybrid is the default
 *  search style — lexical and semantic fused — and the order says so. It came
 *  here with the field: the page below is the answer now, not the form that
 *  asked, so the one copy of this list is the one that asks. */
const MODES: SearchMode[] = ["hybrid", "semantic", "lexical"];

const isMode = (m: unknown): m is SearchMode =>
  m === "lexical" || m === "semantic" || m === "hybrid";

/**
 * The four things a search is. Held together because they are committed
 * together — one address, one question — and because a box that ran a search the
 * moment a letter arrived would ask four questions while someone typed one word.
 */
interface Asked {
  q: string;
  mode: SearchMode;
  person: string;
  since: string;
}

const NOTHING: Asked = { q: "", mode: "hybrid", person: "", since: "" };

/** Whether these four ask anything. A mode with nothing to ask is not a question,
 *  so a mode alone does not count — the home page's rule in router.tsx is the
 *  same one, and this is its other half. */
const asks = (a: Asked) =>
  a.q.trim() !== "" || a.person.trim() !== "" || a.since.trim() !== "";

/** Trimming is what makes "cutover " and "cutover" one question rather than two:
 *  the box keeps what was typed until the address it wrote lands, and the
 *  address is what the field reads afterwards. */
const trim = (a: Asked): Asked => ({
  q: a.q.trim(),
  mode: a.mode,
  person: a.person.trim(),
  since: a.since.trim(),
});

/** The same four questions, said two ways. */
const same = (a: Asked, b: Asked) =>
  a.q.trim() === b.q.trim() &&
  a.mode === b.mode &&
  a.person.trim() === b.person.trim() &&
  a.since.trim() === b.since.trim();

/**
 * The corpus search, at the nav's right end: a box that becomes the row it is on
 * while it is being used.
 *
 * The search is a page — a query, a mode, a person, a date, and a list of
 * candidates to judge — and the four controls that ask it live here, in the one
 * element every page has. The page below is only the answer. Its own form was the
 * second place to type one query, and two boxes writing the same address are two
 * boxes that can be read, at a glance, as disagreeing.
 *
 * Shut, it is a box: what the address says is in it, and nothing else is.
 * Focused, it takes the width of the nav and the options come with it — the links
 * step aside rather than sitting under it, because the row it grows into is a
 * thing being used (see `.navsearch.open` in styles.css). Blurred, what was typed
 * is committed, and the panel folds away only when there is nothing in it: an
 * empty box is a search nobody started, while a question in force is one being
 * read or refined, and folding it away would put its own options behind another
 * click. Escape empties the box and shuts it — the field, the question in force,
 * and the page below, back to the default view — and so does clearing the box by
 * hand. What it does not take with it is the thread the reader has open: the pane
 * is the reading rather than the question, so it stays open on the default view
 * below (see commit).
 *
 * Nothing here navigates on the way in. Opening the search asks nothing, so the
 * list below is left alone — which is what makes an empty box the default view
 * rather than a search page with nothing asked of it.
 */
export function NavSearch() {
  const navigate = useNavigate();
  // What the address already asks, read on every render rather than kept, so a
  // link, a Back or a reload moves the box too. Only the search page can carry a
  // question — every other route has nothing to read, and the box starts empty.
  const asked = useRouterState({
    select: (s): Asked => {
      if (s.location.pathname !== "/") return NOTHING;
      const search = s.location.search as Record<string, unknown>;
      return {
        q: typeof search.q === "string" ? search.q : "",
        mode: isMode(search.mode) ? search.mode : "hybrid",
        person: typeof search.person === "string" ? search.person : "",
        since: typeof search.since === "string" ? search.since : "",
      };
    },
  });

  // The thread the reader has open, read from the same address the four come from
  // and *not* part of them: it belongs to the reading rather than to the question.
  // The panel drops it on the way out of a search (see commit), which is why it
  // is read here rather than kept in state.
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

  // The draft is dropped when the address it wrote has landed — not where the
  // commit happened, or the fields would show the question being replaced for a
  // frame before showing the one that replaces it.
  const address = `${asked.q}\u0000${asked.mode}\u0000${asked.person}\u0000${asked.since}`;
  useEffect(() => {
    setDraft(null);
  }, [address]);

  // The people the corpus holds, for the Person control — asked for only while
  // the panel is open. A corpus of a few hundred people is not a question every
  // page should ask in order to draw a nav.
  const people = $api.useQuery("get", "/v1/people", {}, { enabled: open });

  const edit = (patch: Partial<Asked>) => setDraft({ ...shown, ...patch });

  /**
   * Commit the four: the address becomes the question, and the page below answers
   * it. Nothing is asked when nothing changed — an address that already says this
   * *is* the address, and re-writing it would drop the folder and the open thread
   * the reader is in the middle of for a search that is the same search.
   */
  const commit = (next: Asked) => {
    const four = trim(next);
    if (same(four, asked)) {
      setDraft(null);
      return;
    }
    navigate({
      to: "/",
      search: {
        // Only what was asked is written, so the canonical address of the
        // default search stays plain /. Hybrid is the default, so it is omitted.
        ...(four.q ? { q: four.q } : {}),
        ...(four.mode !== "hybrid" ? { mode: four.mode } : {}),
        ...(four.person ? { person: four.person } : {}),
        ...(four.since ? { since: four.since } : {}),
        // Giving the question up is not giving up the reading. Emptying the box
        // (or Escape) puts the default view back below, and the pane the reader
        // had open is the same pane on either page — so its thread is carried
        // across rather than emptied by a box above it that has nothing in it.
        //
        // Only on the way out, and that asymmetry is the search page's own rule:
        // a commit that still asks something is a new question, whose results are
        // new, and the pane there starts empty until a result is clicked (see
        // Select) — the last question's thread is not an answer to this one.
        ...(asks(four) || openThread === undefined ? {} : { open: openThread }),
      },
      // Replaced, not pushed: the search IS the home page, and Back from a built
      // page (which is pushed) lands straight back on it.
      replace: true,
    });
  };

  return (
    <form
      className={open ? "navsearch open" : "navsearch"}
      role="search"
      onSubmit={(ev) => {
        ev.preventDefault();
        commit(shown);
      }}
      // The caret arriving anywhere in the panel is what opens it, and the panel
      // does not close while focus is inside it — the blur handler below would
      // otherwise shut the field on the way to the mode dropdown.
      onFocus={() => setOpen(true)}
      // And clicking it opens it again even when the caret never left: Escape
      // shuts the panel without moving focus, and a box that could not be opened
      // again would be the same control with one gesture fewer.
      onClick={() => setOpen(true)}
      // Escape empties the box and shuts it, which is Escape's plain sense in a
      // field: what was typed goes, and so does the question the box was showing.
      // A box that emptied its field while the address still asked would fill
      // itself back in the moment it shut, because what a shut box shows is what
      // the address says — so leaving the question behind is not an option, and
      // clearing it asks nothing (`NOTHING`), which puts the default view back
      // below. An address that already asks nothing is left alone entirely: the
      // commit below is a no-op when the four are the same, so a reader who
      // opened an empty box and pressed Escape has not navigated anywhere. It
      // searches nothing — Escape is the way out, not the way in.
      onKeyDown={(ev) => {
        if (ev.key !== "Escape") return;
        setDraft(null);
        setOpen(false);
        commit(NOTHING);
      }}
      onBlur={(ev) => {
        if (ev.currentTarget.contains(ev.relatedTarget as Node | null)) return;
        // Leaving commits what was typed — type and click away and the search
        // runs — and folds the panel away only when there is nothing to ask. A
        // question in force keeps the panel: its options are how it gets refined,
        // and a search that had to be reopened to be narrowed would be a box
        // hiding its own controls.
        commit(shown);
        if (!asks(shown)) setOpen(false);
      }}
    >
      {/* A magnifier rather than nothing: an empty box at the end of a nav is a
          box with no label, and this is the one glyph that says what it takes. */}
      <svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
        <circle cx="7" cy="7" r="4.6" fill="none" stroke="currentColor" strokeWidth="1.5" />
        <path
          d="M10.4 10.4 14.4 14.4"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
        />
      </svg>
      <input
        className="navq"
        value={shown.q}
        onChange={(ev) => {
          edit({ q: ev.target.value });
          // Typing is using the search, so the panel comes back with it: a shut
          // panel with the caret still in the box is where Escape leaves a reader
          // who then starts a new query, and the options are what a query is
          // refined with.
          setOpen(true);
        }}
        // The label the box is read by. Visible it has no label of its own: the
        // glyph and the placeholder are what a person sees, and a word repeated
        // above a twelve-rem field is a word taken from the field.
        aria-label="Search the corpus"
        // The address carries a question, so the box says so the way a current nav
        // item does: `aria-current` is what a screen reader hears, and it is the
        // whole of the mark. There is no rule for it — the text of a live query was
        // coloured the accent, and then filled with `--mine`, which is the colour
        // this app uses for the reader's own mail; a brown field that meant
        // "you are here" was a second meaning for a colour that already had one,
        // and the search box's own background is not a thing a search should
        // change. The focus ring on the box says where the keyboard is, and this
        // says what the address asks.
        aria-current={asks(asked) ? "page" : undefined}
        placeholder="Search…"
        autoComplete="off"
        spellCheck={false}
      />
      {open ? (
        <span className="navopts">
          {/* The options exist while the panel is open and not otherwise: shut,
              the box is the whole interface, and every control beside it is one
              more thing between the reader and the field. */}
          <label className="navopt">
            <span>Mode</span>
            <select
              value={shown.mode}
              onChange={(ev) => {
                const mode = ev.target.value as SearchMode;
                // A mode decides how to ask a question that is already being
                // asked, so it is committed at once — with whatever else is in
                // the panel, which is the question it is deciding about.
                edit({ mode });
                commit({ ...shown, mode });
              }}
            >
              {MODES.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </label>
          <label className="navopt">
            <span>Person</span>
            {/* A field with the corpus's people under it, rather than a list of
                everyone in a dropdown: the people number in the hundreds, and
                what is being looked for is a person whose name the reader
                half-remembers (see NavPerson). What a pick writes is the alias
                the corpus chose, and what is typed and left alone is committed
                as itself — a display name resolves, and a string that names
                nobody matches nobody.

                Capped at 20ch (see .navperson): what it shows is an address,
                which is longer than a name, and this control shares its line
                with the mode and the date — one long value must not be what
                decides how much of that line the others get. */}
            <NavPerson
              people={people.data?.people ?? []}
              value={shown.person}
              // Typing is a draft, not a question: the box asks when it is
              // submitted or left (see commit), which is what keeps a name
              // being typed from being a dozen searches on the way to one.
              onEdit={(person) => edit({ person })}
              // A pick is a decision about a question already being asked, so it
              // commits at once — with whatever else is in the panel, which is
              // that question.
              onCommit={(person) => {
                edit({ person });
                commit({ ...shown, person });
              }}
            />
          </label>
          <label className="navopt">
            <span>Since</span>
            {/* A date, in the browser's own picker: `since` is a date on the wire
                (`format: date` in api/openapi.json), so the control that writes
                one should offer dates rather than take a string and let the
                corpus decide what it meant. The value is the `YYYY-MM-DD` the
                address carries and the picker writes back — no conversion, and
                no second opinion about what a date looks like. No placeholder
                either: the field draws its own format, and a `YYYY-MM-DD` beside
                it would be a second answer to the same question. */}
            <input
              className="navsince"
              type="date"
              value={shown.since}
              onChange={(ev) => {
                const since = ev.target.value;
                // Like the mode and the person: a date decides how a question
                // already being asked is asked, so it commits at once — with
                // whatever else is in the panel, which is that question. A date
                // field cannot hold half a date, so there is no draft worth
                // keeping here while the reader types.
                edit({ since });
                commit({ ...shown, since });
              }}
            />
          </label>
          {/* The way in for anyone not pressing Enter, and what makes Enter work
              at all: a form submits on Enter by way of its submit button. */}
          <button type="submit" disabled={!asks(shown)}>
            Search
          </button>
        </span>
      ) : null}
    </form>
  );
}
