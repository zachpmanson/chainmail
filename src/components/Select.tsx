import { useEffect, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { $api, searchQuery, type ChainHit, type SearchMode, type SearchParams } from "../lib/api";
import { ChainReading, Failure, type PreviewableChain } from "./ChainPreview";
import { useBuildPage } from "../lib/build";
import { SplitPane } from "./SplitPane";

// The default-first order is what the dropdown shows: hybrid is the default
// search style — lexical and semantic fused — and the order says so.
const MODES: SearchMode[] = ["hybrid", "semantic", "lexical"];

/** Both ends of the span, or the one date when a chain never got a reply. */
function span(chain: ChainHit): string {
  const day = (t?: string) => (t ? t.slice(0, 10) : "");
  const a = day(chain.first);
  const b = day(chain.last);
  if (!a && !b) return "undated";
  if (!b || a === b) return a || b;
  return `${a} – ${b}`;
}

/** The search page's relevance floor, for the highlight. A chain whose best
 * cosine clears it is marked as a strong semantic match — the same number
 * refresh holds semantic-only proposals to (see internal/refresh). */
const HIGHLIGHT_FLOOR = 0.8;

/** The chain's best cosine similarity to the query, from its best entry hits. */
function chainSimilarity(chain: ChainHit): number {
  let best = 0;
  for (const e of chain.best ?? []) {
    if (e.semRank > 0 && e.similarity !== undefined && e.similarity > best) best = e.similarity;
  }
  return best;
}

export function ChainRow({
  chain,
  checked,
  current,
  onToggle,
  onPreview,
}: {
  chain: ChainHit;
  checked: boolean;
  /** Whether this is the chain the pane is reading. Absent where the row is
   *  read in a modal instead — a list with no pane has nothing to mark. */
  current?: boolean;
  onToggle: () => void;
  onPreview: () => void;
}) {
  const sim = chainSimilarity(chain);
  const hot = sim > HIGHLIGHT_FLOOR;
  return (
    <li className={`selrow${hot ? " selhot" : ""}${current ? " sel" : ""}`}>
      <label className="chk">
        <input type="checkbox" checked={checked} onChange={onToggle} />
        <span className="seld">
          <span className="selsub">
            {chain.subject || "(no subject)"}
            {hot ? (
              <span className="selshot" title="strong semantic match">
                strong
              </span>
            ) : null}
          </span>
          <span className="selmeta">
            <span className="selratio" title="matching entries of the whole chain">
              {chain.matched} of {chain.entries} matched
            </span>
            {sim > 0 ? (
              <span className="selsim" title="best cosine similarity of the chain">
                sim {sim.toFixed(2)}
              </span>
            ) : null}
            {chain.people > 0 ? (
              <span className="selppl" title="distinct people in the whole chain, senders and recipients">
                {chain.people} participant{chain.people === 1 ? "" : "s"}
              </span>
            ) : null}
            <span className="selspan">{span(chain)}</span>
            {chain.sources?.length ? (
              <span className="selsrc">{chain.sources.join(", ")}</span>
            ) : null}
          </span>
        </span>
      </label>
      {/* Preview reads the chain as data — cheap, no spec assembly — so a
          candidate can be judged on its entries before it is committed to a
          page. It reads it in the pane beside the list rather than in a modal
          over it: judging a candidate is comparing it with the others, which a
          dialog hides. The button is kept out of the checkbox label, so ticking
          a row and reading it never fight over one hit area. */}
      <button type="button" className="selpvbtn" onClick={onPreview}>
        Preview
      </button>
    </li>
  );
}


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
  const [title, setTitle] = useState("");
  const [me, setMe] = useState("");
  // A URL that already names a search (a reload, or Back from a built page)
  // runs it on mount instead of waiting for a submit.
  const [asked, setAsked] = useState<SearchParams | null>(() =>
    urlSearch.q?.trim() || urlSearch.person?.trim() || urlSearch.since?.trim()
      ? { q: urlSearch.q ?? "", mode: urlSearch.mode ?? "hybrid", person: urlSearch.person?.trim() ?? "", since: urlSearch.since?.trim() ?? "" }
      : null,
  );
  const [chosen, setChosen] = useState<string[]>([]);

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
   *
   * Naming the page, sending the request and pushing the URL all live in
   * useBuildPage, because the inbox builds pages too and one of the two views
   * deciding its own name is how they would drift apart.
   */
  const { build, start } = useBuildPage();

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
  const addresses = me
    .split(",")
    .map((a) => a.trim())
    .filter((a) => a !== "");

  // The address may name a chain these results do not hold — a candidate read,
  // then a reload before the answer came back, or an address carried over from
  // another search. The pane reads it from the id either way (the chain is
  // fetched by id), so its head says what is known rather than inventing a
  // subject or a count. With no id at all the pane reads the top of the ranked
  // list, which is what the reader is looking at anyway.
  const picked = chains.find((c) => c.rootExtId === opened);
  const reading: PreviewableChain | null =
    picked ?? (opened ? { rootExtId: opened } : chains[0]) ?? null;

  return (
    <div className="wrap selwrap">
      <form className="selform" onSubmit={submit}>
        <label className="self">
          <span>Query</span>
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="words, a name, an id" />
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
                <ul className="sellist">
                  {chains.map((c) => (
                    <ChainRow
                      key={c.rootExtId}
                      chain={c}
                      checked={chosen.includes(c.rootExtId)}
                      current={reading?.rootExtId === c.rootExtId}
                      onToggle={() => toggle(c.rootExtId)}
                      onPreview={() => openChain(c.rootExtId)}
                    />
                  ))}
                </ul>
              </div>
            }
            pane={
              <aside className="ibread" aria-label="The candidate being read">
                {reading ? (
                  <>
                    <div className="ibread-head">
                      <button type="button" className="ibback" onClick={closeChain}>
                        ← Results
                      </button>
                      <span className="ibread-subj">{reading.subject || "(no subject)"}</span>
                      <span className="note">
                        {reading.entries
                          ? `${reading.entries} entr${reading.entries === 1 ? "y" : "ies"}`
                          : ""}
                      </span>
                    </div>
                    <ChainReading chain={reading} />
                  </>
                ) : (
                  <p className="selnote">Nothing to read yet.</p>
                )}
              </aside>
            }
          />

          {/* The build bar at the foot of the workspace: the search is above,
              the candidates are in the middle, and what to do with the ticked
              ones is the last thing on the page — where the inbox puts its
              own. */}
          <div className="selbuild ibbuild">
            <label className="self">
              <span>Page title</span>
              <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="optional" />
            </label>
            {/* Nothing in the corpus records which mailbox it was collected
                from, so the reader's own messages can only be marked by being
                told which addresses are theirs. */}
            <label className="self">
              <span>Your addresses</span>
              <input value={me} onChange={(e) => setMe(e.target.value)} placeholder="comma separated" />
            </label>
            <button
              type="button"
              disabled={chosen.length === 0 || build.isPending}
              onClick={() =>
                start({
                  chains: chosen,
                  title,
                  me: addresses,
                  // Recorded on the page so a refresh can propose the chains
                  // this query would find now but did not when it was curated.
                  queries: asked ? [{ q: asked.q, note: `corpus search, mode=${asked.mode}` }] : [],
                })
              }
            >
              {build.isPending ? "Building…" : `Build page from ${chosen.length} chain${chosen.length === 1 ? "" : "s"}`}
            </button>
            {/* Seconds of silence reads as a broken page, so the wait says what
                it is waiting on and how much of it there is. */}
            {build.isPending ? (
              <p className="selnote" role="status">
                Recovering HTML and detecting boilerplate across {chosen.length} chain
                {chosen.length === 1 ? "" : "s"}. This takes a few seconds.
              </p>
            ) : null}
            {build.isError ? <Failure error={build.error} /> : null}
          </div>
        </>
      ) : null}
    </div>
  );
}
