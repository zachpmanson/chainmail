import { useState } from "react";
import { ApiError, type ChainHit, type SearchMode, type SearchParams } from "../lib/api";
import { useSearch, useSpecBuild } from "../lib/queries";
import type { Timeline } from "../lib/spec";

const MODES: SearchMode[] = ["lexical", "semantic", "hybrid"];

/**
 * Names the status so two declines are told apart. "Not found" and "Rejected"
 * are different instructions to the person reading them: one means the id names
 * nothing, the other means the query has to change. A single "request failed"
 * would leave them guessing which.
 */
function statusLabel(status: number): string {
  if (status === 404) return "Not found";
  if (status === 400) return "Rejected";
  if (status === 503) return "Service unavailable";
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

function ChainRow({
  chain,
  checked,
  onToggle,
}: {
  chain: ChainHit;
  checked: boolean;
  onToggle: () => void;
}) {
  return (
    <li className="selrow">
      <label className="chk">
        <input type="checkbox" checked={checked} onChange={onToggle} />
        <span className="seld">
          <span className="selsub">{chain.subject || "(no subject)"}</span>
          <span className="selmeta">
            {/* The ratio, not the numerator: 3 of 4 entries is a thread about
                the query, 3 of 180 is a thread that mentioned it once. Showing
                only "3 matched" makes those two look identical. */}
            <span className="selratio" title="matching entries of the whole chain">
              {chain.matched} of {chain.entries} matched
            </span>
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
 */
export function SelectView({ onBuilt }: { onBuilt: (spec: Timeline) => void }) {
  const [q, setQ] = useState("");
  const [mode, setMode] = useState<SearchMode>("lexical");
  const [person, setPerson] = useState("");
  const [since, setSince] = useState("");
  const [title, setTitle] = useState("");
  const [me, setMe] = useState("");
  const [asked, setAsked] = useState<SearchParams | null>(null);
  const [chosen, setChosen] = useState<string[]>([]);

  const results = useSearch(asked);
  const build = useSpecBuild(onBuilt);

  const submit = (ev: React.FormEvent) => {
    ev.preventDefault();
    // A new search invalidates the selection: the ids chosen were chosen out of
    // the old candidate list, and carrying them forward would build a page from
    // chains no longer on screen.
    setChosen([]);
    setAsked({ q, mode, person: person.trim(), since: since.trim() });
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
        <button type="submit" disabled={q.trim() === ""}>
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
              onClick={() =>
                build.mutate({
                  chains: chosen,
                  ...(title.trim() ? { title: title.trim() } : {}),
                  ...(addresses.length ? { me: addresses } : {}),
                  // Recorded on the page so a refresh can propose the chains
                  // this query would find now but did not when it was curated.
                  ...(asked ? { queries: [{ q: asked.q, note: `corpus search, mode=${asked.mode}` }] } : {}),
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
