import { useRef, useState } from "react";
import { useNavigate, useSearch, Link } from "@tanstack/react-router";
import { $api, ApiError, searchQuery, type ChainHit, type SearchMode, type SearchParams } from "../lib/api";
import { slug, untitledName } from "../lib/route";

// The default-first order is what the dropdown shows: hybrid is the default
// search style — lexical and semantic fused — and the order says so.
const MODES: SearchMode[] = ["hybrid", "semantic", "lexical"];

/**
 * Names the status so two declines are told apart. "Not found" and "Rejected"
 * are different instructions to the person reading them: one means the id names
 * nothing, the other means the query has to change. A single "request failed"
 * would leave them guessing which.
 */
function statusLabel(status: number): string {
  if (status === 404) return "Not found";
  if (status === 400) return "Rejected";
  if (status === 429) return "Busy";
  if (status === 503) return "Service unavailable";
  if (status === 504) return "Timed out";
  return `Error ${status}`;
}

function Failure({ error }: { error: unknown }) {
  const api = error instanceof ApiError ? error : null;
  return (
    <p className="selfail" role="alert">
      <strong>{api ? `${statusLabel(api.status)} (${api.status})` : "Could not reach the service"}</strong>{" "}
      <span>{error instanceof Error ? error.message : String(error)}</span>
    </p>
  );
}

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

function ChainRow({
  chain,
  checked,
  onToggle,
}: {
  chain: ChainHit;
  checked: boolean;
  onToggle: () => void;
}) {
  const sim = chainSimilarity(chain);
  const hot = sim > HIGHLIGHT_FLOOR;
  return (
    <li className={`selrow${hot ? " selhot" : ""}`}>
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
  // The name is settled at click time, in one place, so the URL that is pushed
  // and the file the server saves can never disagree. A clock name for an
  // untitled page is generated per click for the same reason: two builds must
  // not overwrite each other silently.
  const pendingName = useRef("");
  const build = $api.useMutation("post", "/v1/spec", {
    // The saved page's URL is the name the client chose, always: the server
    // saves exactly `name` from the request and returns it as the title (it may
    // borrow a subject when none was given, but that borrows a title, not a
    // file name). Reslugging the returned title would point the address bar at
    // a file that was never written — a titleless build saves under the clock
    // name but announces the borrowed subject's slug. The request name IS the
    // saved file, so it IS the URL.
    onSuccess: () =>
      navigate({
        to: "/view/$name",
        params: { name: pendingName.current },
      }),
  });

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

  return (
    <div className="wrap selwrap">
      <header className="top">
        <h1>chainmail</h1>
        <p className="sub">Search the corpus, choose the chains that belong, then build the page.</p>
        <p className="selnote"><Link to="/status">Services</Link></p>
      </header>

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
          <div className="selbuild">
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
              onClick={() => {
                pendingName.current = slug(title.trim()) || untitledName();
                build.mutate({
                  body: {
                    chains: chosen,
                    name: pendingName.current,
                    ...(title.trim() ? { title: title.trim() } : {}),
                    ...(addresses.length ? { me: addresses } : {}),
                    // Recorded on the page so a refresh can propose the chains
                    // this query would find now but did not when it was curated.
                    ...(asked
                      ? { queries: [{ q: asked.q, note: `corpus search, mode=${asked.mode}` }] }
                      : {}),
                  },
                });
              }}
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

          <ul className="sellist">
            {chains.map((c) => (
              <ChainRow
                key={c.rootExtId}
                chain={c}
                checked={chosen.includes(c.rootExtId)}
                onToggle={() => toggle(c.rootExtId)}
              />
            ))}
          </ul>
        </>
      ) : null}
    </div>
  );
}
