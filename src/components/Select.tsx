import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { $api, searchQuery, type SearchParams } from "../lib/api";
import { useLastDescription } from "../lib/lists";
import { useEscapeToClear } from "../lib/selection";
import { ActionBar } from "./ActionBar";
import { ThreadPane } from "./ThreadPane";
import { ThreadRow, RankMeta } from "./ThreadRow";
import { Failure, type PreviewableThread } from "./ThreadPreview";
import { SplitPane } from "./SplitPane";

/**
 * Search, then choose, then build. Selection is a stage of its own because
 * dropping a thread after the fact means rebuilding the page and everything
 * derived from it, so scope is settled once, before anything is generated.
 *
 * The question itself is asked in the nav (see NavSearch), which is the one
 * element every page has: this page is the answer to whatever the address says,
 * and the address is written by the box up there. So there is no form here — no
 * second copy of the query to fall out of step with the one that was answered.
 *
 * Building names the page (from the title, or a clock name when it has none)
 * and navigates to /view/<name>: the router owns the URL, the page is saved
 * server-side, so it survives a refresh.
 */
export function SelectView() {
  const navigate = useNavigate();
  // The search lives in the URL (q, mode, person, since), validated and typed by
  // the route: leaving for a built page and pressing Back restores the search
  // that was there before, even after a reload.
  const urlSearch = useSearch({ from: "/" });
  const q = urlSearch.q?.trim() ?? "";
  const mode = urlSearch.mode ?? "hybrid";
  const person = urlSearch.person?.trim() ?? "";
  const since = urlSearch.since?.trim() ?? "";
  // The four that ask something, or null. A mode alone is not a question — the
  // home page does not send one here (see router.tsx) — so it is not counted.
  const asked: SearchParams | null = q || person || since ? { q, mode, person, since } : null;
  const [chosen, setChosen] = useState<string[]>([]);
  // The same key answers the same state here as on the inbox: Escape drops the
  // ticks, except while the search box or the folder menu has the focus, where it
  // closes those instead (see lib/selection.ts).
  const clearChosen = useCallback(() => setChosen([]), []);
  useEscapeToClear(chosen.length > 0, clearChosen);
  // A new question is a new candidate list, so the ticks from the old one go: the
  // ids chosen were chosen out of a list that is no longer on screen. Kept beside
  // the question being asked rather than cleared where the search is committed,
  // because the commit happens in the nav, which knows nothing about this page's
  // selection.
  const question = [q, mode, person, since].join("\u0000");
  const lastQuestion = useRef(question);
  useEffect(() => {
    if (lastQuestion.current === question) return;
    lastQuestion.current = question;
    setChosen([]);
  }, [question]);

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
  // the thread that was being read, not at the top of the results again. Absent
  // means nothing was picked, and then the pane reads the first result — the top
  // of a ranked list is what the reader is looking at anyway.
  const opened = useSearch({ from: "/" }).open;
  const openChain = (root: string) =>
    navigate({ to: "/", search: (prev) => ({ ...prev, open: root }) });
  const closeChain = () => navigate({ to: "/", search: (prev) => ({ ...prev, open: undefined }) });

  // Nothing is fetched until the address asks something. This page is only ever
  // mounted with a question on it (the home page shows the inbox otherwise), and
  // the idle init is never sent: it stands in so the key it derives is a key
  // nothing was ever fetched under.
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

  const toggle = (root: string) =>
    setChosen((prev) => (prev.includes(root) ? prev.filter((r) => r !== root) : [...prev, root]));

  const chains = results.data?.chains ?? [];

  // The address may name a thread these results do not hold — a candidate read,
  // then a reload before the answer came back, or an address carried over from
  // another search. The pane reads it from the id either way (the thread is
  // fetched by id), so its head says what is known rather than inventing a
  // subject or a count. With no id at all **nothing is open**: the pane starts
  // empty and stays empty until a result is clicked. The top of the ranking is
  // not a choice anyone made, and a pane that filled itself in would be a
  // candidate the reader has to dismiss before judging any of them.
  // As on the inbox: the results may no longer hold the candidate being read —
  // moving it out of the folder the search asked for is a write that takes it out of
  // these results (see dropFromLists) — and the pane's head is about the mail, not
  // about the list it was found in (see useLastDescription).
  const picked = useLastDescription(opened ?? null, chains.find((c) => c.rootExtId === opened) ?? null);
  const reading: PreviewableThread | null = opened ? picked ?? { rootExtId: opened } : null;

  return (
    <div className="wrap selwrap">
      {results.isError ? <Failure error={results.error} /> : null}
      {results.isFetching ? <p className="selnote">Searching…</p> : null}
      {asked && !results.isFetching && !results.isError && chains.length === 0 ? (
        <p className="selnote">No thread matched.</p>
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
                    <ThreadRow
                      key={c.rootExtId}
                      thread={c}
                      checked={chosen.includes(c.rootExtId)}
                      current={reading?.rootExtId === c.rootExtId}
                      meta={<RankMeta thread={c} />}
                      onToggle={() => toggle(c.rootExtId)}
                      // The row body opens the candidate in the pane, which is
                      // what it does on the inbox too: a ranked list is still a
                      // list of chains, and a second "Preview" control beside it
                      // was two ways to do the one thing.
                      onOpen={() => openChain(c.rootExtId)}
                    />
                  ))}
                </ul>
              </div>
            }
            pane={
              <ThreadPane
                thread={reading}
                label="The candidate being read"
                backLabel="← Results"
                empty="Nothing open — pick a result from the list."
                onClose={closeChain}
              />
            }
          />

          {/* The bar of things to do with the ticked candidates, at the foot of
              the workspace: the search is above in the nav, the candidates are
              in the middle, and what to do with the ticked ones is last — the
              same bar, and the same rules, as the inbox's. What this page knows
              and the inbox does not is the query that found them, which the page
              records so a refresh can propose what it would find now. */}
          <ActionBar
            chosen={chosen}
            queries={asked ? [{ q: asked.q, note: `corpus search, mode=${asked.mode}` }] : []}
            onDone={clearChosen}
          />
        </>
      ) : null}
    </div>
  );
}
