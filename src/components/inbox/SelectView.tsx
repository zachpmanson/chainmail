import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { $api, searchQuery, type SearchParams } from "../../lib/api/api";
import { usePrefs } from "../../lib/prefs/usePrefs";
import { useLastDescription } from "../../lib/inbox/lists";
import { useEscapeToClear } from "../../lib/inbox/selection";
import ActionBar from "./ActionBar";
import ThreadPane from "../thread/ThreadPane";
import CompactThreadRow from "./CompactThreadRow";
import FullThreadRow from "./FullThreadRow";
import RankMeta from "./RankMeta";
import Failure from "../thread/Failure";
import { type PreviewableThread } from "../thread/ThreadShared";
import SplitPane from "./SplitPane";
import CompactListHeader from "./CompactListHeader";
import CompactModeToggle from "./CompactModeToggle";
import ComposeBox from "../compose/ComposeBox";
import { useCompose } from "../compose/ComposeContext";
import ThreadListScroll from "./ThreadListScroll";

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
  const [chosen, setChosen] = useState<string[]>([]);
  const clearChosen = useCallback(() => setChosen([]), []);
  useEscapeToClear(chosen.length > 0, clearChosen);
  // A new question clears the ticks. Done here because the commit happens in the nav,
  // which doesn't know about this selection.
  const question = [q, mode, person, since, accountId].join("\u0000");
  const lastQuestion = useRef(question);
  useEffect(() => {
    if (lastQuestion.current === question) return;
    lastQuestion.current = question;
    setChosen([]);
  }, [question]);

  useEffect(() => {
    document.body.classList.add("search");
    return () => document.body.classList.remove("search");
  }, []);

  const opened = useSearch({ from: "/" }).open;
  // Choosing a candidate dismisses compose, or the thread would open hidden beneath it.
  const openChain = (root: string) => {
    closeCompose();
    if (root === opened) return;
    navigate({ to: "/", search: (prev) => ({ ...prev, open: root }) });
  };
  const closeChain = () => navigate({ to: "/", search: (prev) => ({ ...prev, open: undefined }) });

  // The idle init is never sent; it only gives the disabled query a key.
  const results = $api.useQuery(
    "get",
    "/v1/search",
    { params: { query: asked ? searchQuery(asked) : {} } },
    { enabled: asked !== null },
  );

  const toggle = (root: string) =>
    setChosen((prev) => (prev.includes(root) ? prev.filter((r) => r !== root) : [...prev, root]));

  const chains = results.data?.chains ?? [];

  // The opened thread may not be in these results (e.g. moved out by a write, see
  // dropFromLists); the pane fetches it by id and keeps the last known description.
  const picked = useLastDescription(
    opened ?? null,
    chains.find((c) => c.rootExtId === opened) ?? null,
  );
  const reading: PreviewableThread | null = opened ? (picked ?? { rootExtId: opened }) : null;

  return (
    <div className="wrap selwrap mx-0 w-full max-w-none p-0 min-[60rem]:flex min-[60rem]:flex-1 min-[60rem]:flex-col min-[60rem]:min-h-0">
      {results.isError ? <Failure error={results.error} /> : null}
      {results.isFetching ? (
        <p className="mt-2 flex-[1_1_100%] text-xs text-muted">Searching…</p>
      ) : null}
      {asked && !results.isFetching && !results.isError && chains.length === 0 ? (
        <p className="mt-2 flex-[1_1_100%] text-xs text-muted">No thread matched.</p>
      ) : null}

      {chains.length > 0 ? (
        <>
          <SplitPane
            hasChoice={composing || Boolean(opened)}
            list={
              <ThreadListScroll>
                <div className="flex items-center gap-2 p-2">
                  <CompactModeToggle compact={compact} onChange={setCompact} />
                </div>
                {compact ? <CompactListHeader /> : null}
                <ul className="m-0 list-none divide-y divide-line overflow-hidden rounded-lg border border-line bg-card p-0 min-[60rem]:border-0 min-[60rem]:rounded-none min-[60rem]:bg-transparent">
                  {chains.map((c) => (
                    <Row
                      key={c.rootExtId}
                      thread={c}
                      checked={chosen.includes(c.rootExtId)}
                      current={!composing && reading?.rootExtId === c.rootExtId}
                      meta={<RankMeta thread={c} />}
                      onToggle={() => toggle(c.rootExtId)}
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
