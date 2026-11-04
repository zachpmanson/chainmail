import { useEffect } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { $api, searchQuery, type SearchParams } from "../../lib/api/api";
import { usePrefs } from "../../lib/prefs/usePrefs";
import { useLastDescription } from "../../lib/inbox/lists";
import { useEscapeToClear } from "../../lib/inbox/selection";
import useScopedSelection from "../../lib/inbox/useScopedSelection";
import ActionBar from "./ActionBar";
import ThreadPane from "../thread/ThreadPane";
import CompactThreadRow from "./CompactThreadRow";
import FullThreadRow from "./FullThreadRow";
import RankMeta from "./RankMeta";
import Failure from "../thread/Failure";
import { type PreviewableThread } from "../thread/ThreadShared";
import SplitPane from "./SplitPane";
import ThreadListHeader from "./ThreadListHeader";
import CompactModeToggle from "./CompactModeToggle";
import ComposeBox from "../compose/ComposeBox";
import { useCompose } from "../compose/ComposeContext";
import ThreadListScroll from "./ThreadListScroll";
import { openThread } from "../../lib/ui/openSelection";

export default function SelectView() {
  const navigate = useNavigate();
  const { composing, closeCompose } = useCompose();
  const compact = usePrefs((s) => s.compact);
  const setCompact = usePrefs((s) => s.setCompact);
  const Row = compact ? CompactThreadRow : FullThreadRow;
  const urlSearch = useSearch({ from: "/" });
  const q = urlSearch.q?.trim() ?? "";
  const mode = urlSearch.mode ?? "hybrid";
  const person = urlSearch.person?.trim() ?? "";
  const since = urlSearch.since?.trim() ?? "";
  const accountId = urlSearch.accountId ?? "";
  // A mode alone is not a question (see router.tsx).
  const asked: SearchParams | null =
    q || person || since ? { q, mode, person, since, accountId } : null;

  useEffect(() => {
    document.body.classList.add("search");
    return () => document.body.classList.remove("search");
  }, []);

  const opened = openThread(useSearch({ from: "/" }).open);
  // Choosing a candidate dismisses compose, or the thread would open hidden beneath it.
  const openChain = (root: string) => {
    if (root === opened && !urlSearch.draft) return;
    navigate({
      to: "/",
      search: (prev) => ({ ...prev, open: root, draft: undefined, popup: undefined }),
    });
  };
  const closeChain = () =>
    navigate({ to: "/", search: (prev) => ({ ...prev, open: undefined, draft: undefined }) });

  // The idle init is never sent; it only gives the disabled query a key.
  const results = $api.useQuery(
    "get",
    "/v1/search",
    { params: { query: asked ? searchQuery(asked) : {} } },
    { enabled: asked !== null },
  );

  const chains = results.data?.chains ?? [];
  const { chosen, clearChosen, toggle, toggleAll, allSelected, mixed, canClear } =
    useScopedSelection(
      JSON.stringify([q, mode, person, since, accountId, urlSearch.label]),
      chains.map((chain) => chain.rootExtId),
    );
  useEscapeToClear(canClear, clearChosen);

  // The opened thread may not be in these results (e.g. moved out by a write, see
  // dropFromLists); the pane fetches it by id and keeps the last known description.
  const picked = useLastDescription(
    opened ?? null,
    chains.find((c) => c.rootExtId === opened) ?? null,
  );
  const reading: PreviewableThread | null = opened ? (picked ?? { rootExtId: opened }) : null;

  return (
    <div className="wrap mx-0 w-full max-w-none p-0 min-[60rem]:flex min-[60rem]:min-h-0 min-[60rem]:flex-auto min-[60rem]:flex-col">
      {results.isError ? <Failure error={results.error} /> : null}
      {results.isFetching ? (
        <p className="mt-2 flex-[1_1_100%] text-xs text-muted">Searching…</p>
      ) : null}
      {asked && !results.isFetching && !results.isError && chains.length === 0 ? (
        <p className="mt-2 flex-[1_1_100%] text-xs text-muted">No thread matched.</p>
      ) : null}

      {chains.length > 0 || opened || composing ? (
        <>
          <SplitPane
            hasChoice={composing || Boolean(opened)}
            list={
              <ThreadListScroll>
                <div className="flex items-center gap-2 p-2">
                  <CompactModeToggle compact={compact} onChange={setCompact} />
                </div>
                {chains.length > 0 ? (
                  <ThreadListHeader
                    compact={compact}
                    checked={allSelected}
                    mixed={mixed}
                    onChange={toggleAll}
                  />
                ) : null}
                <ul className="m-0 list-none divide-y divide-line overflow-hidden rounded-lg border border-line bg-card p-0 min-[60rem]:rounded-none min-[60rem]:border-0 min-[60rem]:bg-transparent">
                  {chains.map((c) => (
                    <Row
                      key={c.rootExtId}
                      thread={c}
                      checked={chosen.includes(c.rootExtId)}
                      current={!composing && reading?.rootExtId === c.rootExtId}
                      meta={<RankMeta thread={c} />}
                      onToggle={(extend, checked) => toggle(c.rootExtId, extend, checked)}
                      onOpen={() => openChain(c.rootExtId)}
                    />
                  ))}
                </ul>
              </ThreadListScroll>
            }
            pane={
              composing ? (
                <ComposeBox onClose={closeCompose} />
              ) : (
                <ThreadPane
                  thread={reading}
                  label="The candidate being read"
                  backLabel="← Results"
                  empty="Nothing open — pick a result from the list."
                  onClose={closeChain}
                />
              )
            }
          />

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
