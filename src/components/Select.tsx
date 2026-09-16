import { useEffect, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { $api, searchQuery, type SearchMode, type SearchParams } from "../lib/api";
import { ActionBar } from "./ActionBar";
import { ChainPane } from "./ChainPane";
import { ChainRow, RankMeta } from "./ChainRow";
import { Failure, type PreviewableChain } from "./ChainPreview";
import { SplitPane } from "./SplitPane";

// The default-first order is what the dropdown shows: hybrid is the default
// search style — lexical and semantic fused — and the order says so.
const MODES: SearchMode[] = ["hybrid", "semantic", "lexical"];


/**
 * Search, then choose, then build. Selection is a stage of its own because
 * dropping a chain after the fact means rebuilding the page and everything
 * derived from it, so scope is settled once, before anything is generated.
 *
 * Building names the page (from the title, or a clock name when it has none)
 * and navigates to /view/<name>: the router owns the URL, the page is saved
 * server-side, so it survives a refresh.
 */
export function SelectView() {
  const navigate = useNavigate();
  // The search lives in the URL (q, mode, person, since), validated and typed
  // by the route: leaving for a built page and pressing Back restores the
  // search that was there before, even after a reload.
  const urlSearch = useSearch({ from: "/" });
  const [q, setQ] = useState(urlSearch.q ?? "");
  const [mode, setMode] = useState<SearchMode>(urlSearch.mode ?? "hybrid");
  const [person, setPerson] = useState(urlSearch.person ?? "");
  const [since, setSince] = useState(urlSearch.since ?? "");
  // A URL that already names a search (a reload, or Back from a built page)
  // runs it on mount instead of waiting for a submit.
  const [asked, setAsked] = useState<SearchParams | null>(() =>
    urlSearch.q?.trim() || urlSearch.person?.trim() || urlSearch.since?.trim()
      ? { q: urlSearch.q ?? "", mode: urlSearch.mode ?? "hybrid", person: urlSearch.person?.trim() ?? "", since: urlSearch.since?.trim() ?? "" }
      : null,
  );
  const [chosen, setChosen] = useState<string[]>([]);
  // Whether this visit arrived with a question already asked, read once so the
  // answer belongs to the mount rather than to the render: the nav opens the
  // search page empty and its field is where the caret belongs, while an arrival
  // that names a query is a visit for reading results.
  const [arrivedAsking] = useState(asked !== null);

  // This page is a workspace too: a list to pick candidates out of and a pane to
  // read them in, each scrolling inside the window rather than the page scrolling
  // under them. A fact about the page, so it is said on the body and taken off
  // when the reader leaves.
  useEffect(() => {
    document.body.classList.add("search");
    return () => document.body.classList.remove("search");
  }, []);

  // Which candidate the pane is reading. The URL's, for the same reason the
  // inbox's is: a reload, a shared address or the browser's Back should land on
  // the chain that was being read, not at the top of the results again. Absent
  // means nothing was picked, and then the pane reads the first result — the top
  // of a ranked list is what the reader is looking at anyway.
  const opened = useSearch({ from: "/" }).open;
  const openChain = (root: string) =>
    navigate({ to: "/", search: (prev) => ({ ...prev, open: root }) });
  const closeChain = () => navigate({ to: "/", search: (prev) => ({ ...prev, open: undefined }) });

  // The idle init is never sent: it stands in until a search is submitted, so
  // the key it derives is a key nothing was ever fetched under.
  const results = $api.useQuery(
    "get",
    "/v1/search",
    { params: { query: asked ? searchQuery(asked) : {} } },
    { enabled: asked !== null },
  );

  /**
   * Building a spec runs HTML recovery and boilerplate detection over the whole
   * selection, which takes seconds. A mutation, so it fires when a person asks
   * for a page and never as a side effect of ticking a box — a selection of
   * twelve chains would otherwise queue twelve builds and render the eleventh's
   * answer.
   *
   * The response goes through normalise() for the same reason a file-loaded spec
   * does: the renderer downstream is entitled to see exactly one shape whatever
   * produced it, and its shape is the one in spec.d.ts, generated from
   * schema/timeline.schema.json rather than from the service's inlined copy.
   * Normalising here rather than around the request still reports a spec that
   * will not normalise as the build's own failure, which is where a person
   * looking at the button expects to be told.
   */

  const submit = (ev: React.FormEvent) => {
    ev.preventDefault();
    // A new search invalidates the selection: the ids chosen were chosen out of
    // the old candidate list, and carrying them forward would build a page from
    // chains no longer on screen.
    setChosen([]);
    setAsked({ q, mode, person: person.trim(), since: since.trim() });
    // The URL is replaced, not pushed: the search IS the home page, and Back
    // from a built page (which is pushed) lands straight back on it. Only the
    // non-default mode is written, so the canonical URL for the default search
    // is plain /. Hybrid is the default, so it is omitted.
    navigate({
      to: "/",
      search: {
        ...(q.trim() ? { q: q.trim() } : {}),
        ...(mode !== "hybrid" ? { mode } : {}),
        ...(person.trim() ? { person: person.trim() } : {}),
        ...(since.trim() ? { since: since.trim() } : {}),
      },
      replace: true,
    });
  };

  const toggle = (root: string) =>
    setChosen((prev) => (prev.includes(root) ? prev.filter((r) => r !== root) : [...prev, root]));

  const chains = results.data?.chains ?? [];

  // The address may name a chain these results do not hold — a candidate read,
  // then a reload before the answer came back, or an address carried over from
  // another search. The pane reads it from the id either way (the chain is
  // fetched by id), so its head says what is known rather than inventing a
  // subject or a count. With no id at all **nothing is open**: the pane starts
  // empty and stays empty until a result is clicked. The top of the ranking is
  // not a choice anyone made, and a pane that filled itself in would be a
  // candidate the reader has to dismiss before judging any of them.
  const picked = chains.find((c) => c.rootExtId === opened);
  const reading: PreviewableChain | null =
    picked ?? (opened ? { rootExtId: opened } : null);

  return (
    <div className="wrap selwrap">
      <form className="selform" onSubmit={submit}>
        <label className="self">
          <span>Query</span>
          {/* The caret is in the field when the page is opened with nothing asked
              of it — the nav's button opens the search to be typed in, and a page
              that opens a form without putting the caret in it is a form you have
              to click before you can use it. An arrival that already asked a
              question keeps the caret where it was: that visit is for reading the
              results. */}
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="words, a name, an id"
            autoFocus={!arrivedAsking}
          />
        </label>
        <label className="self">
          <span>Mode</span>
          <select value={mode} onChange={(e) => setMode(e.target.value as SearchMode)}>
            {MODES.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
        </label>
        <label className="self">
          <span>Person</span>
          <input value={person} onChange={(e) => setPerson(e.target.value)} placeholder="optional" />
        </label>
        <label className="self">
          <span>Since</span>
          <input value={since} onChange={(e) => setSince(e.target.value)} placeholder="YYYY-MM-DD" />
        </label>
        <button type="submit" disabled={!q.trim() && !person.trim() && !since.trim()}>
          Search
        </button>
      </form>

      {results.isError ? <Failure error={results.error} /> : null}
      {results.isFetching ? <p className="selnote">Searching…</p> : null}
      {asked && !results.isFetching && !results.isError && chains.length === 0 ? (
        <p className="selnote">No chain matched.</p>
      ) : null}

      {chains.length > 0 ? (
        <>
          {/* Candidates on the left, the one being read on the right: the same
              split the inbox uses, because judging a candidate is comparing it
              with the others — which a modal over the list hides. */}
          <SplitPane
            hasChoice={Boolean(opened)}
            list={
              <div className="iblistwrap">
                <ul className="iblist">
                  {chains.map((c) => (
                    <ChainRow
                      key={c.rootExtId}
                      chain={c}
                      checked={chosen.includes(c.rootExtId)}
                      current={reading?.rootExtId === c.rootExtId}
                      meta={<RankMeta chain={c} />}
                      onToggle={() => toggle(c.rootExtId)}
                      // The row body opens the candidate in the pane, which is
                      // what it does on the inbox too: a ranked list is still a
                      // list of chains, and a second "Preview" control beside it
                      // was two ways to do the one thing.
                      onOpen={() => (opened === c.rootExtId ? closeChain() : openChain(c.rootExtId))}
                    />
                  ))}
                </ul>
              </div>
            }
            pane={
              <ChainPane
                chain={reading}
                label="The candidate being read"
                backLabel="← Results"
                empty="Nothing open — pick a result from the list."
                onClose={closeChain}
              />
            }
          />

          {/* The bar of things to do with the ticked candidates, at the foot of
              the workspace: the search is above, the candidates are in the
              middle, and what to do with the ticked ones is last — the same bar,
              and the same rules, as the inbox's. What this page knows and the
              inbox does not is the query that found them, which the page records
              so a refresh can propose what it would find now. */}
          <ActionBar
            chosen={chosen}
            queries={asked ? [{ q: asked.q, note: `corpus search, mode=${asked.mode}` }] : []}
            onDone={() => setChosen([])}
          />
        </>
      ) : null}
    </div>
  );
}
