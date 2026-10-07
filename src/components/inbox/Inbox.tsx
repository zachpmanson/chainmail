import Button from "../ui/Button";
import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { usePrefs } from "../../lib/prefs/usePrefs";
import { useEscapeToClear } from "../../lib/inbox/selection";
import useFolder from "../../lib/inbox/useFolder";
import useThreadList from "../../lib/inbox/useThreadList";
import useLoadMore from "../../lib/ui/useLoadMore";
import ActionBar from "./ActionBar";
import { useLastDescription } from "../../lib/inbox/lists";
import ThreadPane from "../thread/ThreadPane";
import CompactThreadRow from "./CompactThreadRow";
import FullThreadRow from "./FullThreadRow";
import Failure from "../thread/Failure";
import { type PreviewableThread } from "../thread/ThreadShared";
import SplitPane from "./SplitPane";
import CompactListHeader from "./CompactListHeader";
import ComposeBox from "../compose/ComposeBox";
import { useCompose } from "../compose/ComposeContext";
import FolderPicker from "./FolderPicker";
import ThreadListScroll from "./ThreadListScroll";

export default function Inbox() {
  const navigate = useNavigate();
  const { composing, closeCompose } = useCompose();
  const compact = usePrefs((s) => s.compact);
  const Row = compact ? CompactThreadRow : FullThreadRow;
  const [chosen, setChosen] = useState<string[]>([]);
  const clearChosen = useCallback(() => setChosen([]), []);
  useEscapeToClear(chosen.length > 0, clearChosen);

  useEffect(() => {
    document.body.classList.add("inbox");
    return () => document.body.classList.remove("inbox");
  }, []);

  // Absent means nothing is open; the pane never fills itself with the top thread.
  const opened = useSearch({ from: "/" }).open;
  const folder = useFolder();
  const { label } = folder;
  // Waits for settings so the default folder doesn't swap in after All mail.
  const { list: inbox, rows } = useThreadList(label, folder.accountId, folder.settled);

  // Choosing a thread dismisses compose, or the thread would open hidden beneath it.
  const openChain = (root: string) => {
    closeCompose();
    if (root === opened) return;
    navigate({ to: "/", search: (prev) => ({ ...prev, open: root }) });
  };
  const closeChain = () => navigate({ to: "/", search: (prev) => ({ ...prev, open: undefined }) });

  const toggle = (root: string) =>
    setChosen((prev) => (prev.includes(root) ? prev.filter((r) => r !== root) : [...prev, root]));

  // The opened thread may not be on a loaded page; the pane fetches it by id.
  const onlyID: PreviewableThread = { rootExtId: opened ?? "" };
  // Keeps the subject on the pane's head after a write moves the thread out of view.
  const described = useLastDescription(
    opened ?? null,
    rows.find((c) => c.rootExtId === opened) ?? null,
  );
  const selected: PreviewableThread | null = opened ? (described ?? onlyID) : null;

  // Scroll to a deep-linked row once per thread, so arriving pages don't pull the
  // reader back to it.
  const scrolledTo = useRef<string | null>(null);
  const hasOpenRow = rows.some((c) => c.rootExtId === opened);
  useEffect(() => {
    if (!opened) {
      scrolledTo.current = null;
      return;
    }
    if (scrolledTo.current === opened || !hasOpenRow) return;
    const row = [...document.querySelectorAll<HTMLElement>("li[data-root]")].find(
      (r) => r.dataset.root === opened,
    );
    if (!row) return;
    scrolledTo.current = opened;
    row.scrollIntoView?.({ block: "nearest" });
  }, [opened, hasOpenRow]);

  // The sentinel isn't rendered after a failed page, or it would refire every render.
  const end = useRef<HTMLDivElement | null>(null);
  const paging = inbox.hasNextPage === true && inbox.isFetchNextPageError !== true;
  useLoadMore(end, paging, inbox.fetchNextPage);

  return (
    <div className="wrap mx-0 w-full max-w-none p-0 min-[60rem]:flex min-[60rem]:min-h-0 min-[60rem]:flex-auto min-[60rem]:flex-col">
      {inbox.isError && !inbox.data ? <Failure error={inbox.error} /> : null}
      {!inbox.isPending && !inbox.isError && rows.length === 0 ? (
        <p className="mt-2 flex-[1_1_100%] text-xs text-muted">
          {label ? (
            <>Nothing in {label}.</>
          ) : (
            <>
              Nothing in the corpus yet — <code>corpus slurp</code> ingests the mailbox.
            </>
          )}
        </p>
      ) : null}

      <SplitPane
        hasChoice={composing || Boolean(opened)}
        list={
          <>
            <FolderPicker
              current={label}
              currentAccountId={folder.accountId}
              isDefault={folder.isDefault}
              onPick={folder.pick}
              onDefault={folder.makeDefault}
            />
            <ThreadListScroll>
              {compact && rows.length > 0 ? <CompactListHeader /> : null}
              {rows.length > 0 ? (
                <ul className="m-0 list-none divide-y divide-line overflow-hidden rounded-lg border border-line bg-card p-0 min-[60rem]:rounded-none min-[60rem]:border-0 min-[60rem]:bg-transparent">
                  {rows.map((c) => (
                    <Row
                      key={c.rootExtId}
                      thread={c}
                      checked={chosen.includes(c.rootExtId)}
                      // Not current while composing: the pane isn't showing it, and ThreadRow's press
                      // relies on this to take the pane back.
                      current={!composing && selected?.rootExtId === c.rootExtId}
                      onToggle={() => toggle(c.rootExtId)}
                      onOpen={() => openChain(c.rootExtId)}
                    />
                  ))}
                </ul>
              ) : null}

              {paging ? (
                <div className="py-2 text-center" ref={end}>
                  {inbox.isFetchingNextPage ? (
                    <p className="m-0 flex-[1_1_100%] text-xs text-muted" role="status">
                      Reading further back…
                    </p>
                  ) : null}
                </div>
              ) : null}
              {inbox.isFetchNextPageError ? (
                <>
                  <Failure error={inbox.error} />
                  <Button
                    type="button"
                    className="mx-auto mt-3 block rounded-full px-4 py-1.5 text-xs text-muted hover:border-accent hover:text-accent"
                    onClick={() => inbox.fetchNextPage()}
                  >
                    Try again
                  </Button>
                </>
              ) : null}
            </ThreadListScroll>
          </>
        }
        pane={
          composing ? (
            <ComposeBox onClose={closeCompose} />
          ) : (
            <ThreadPane
              thread={selected}
              label="The selected thread"
              backLabel="← List"
              empty="Nothing open — pick a thread from the list."
              moveDefault={label}
              onClose={closeChain}
            />
          )
        }
      />

      <ActionBar chosen={chosen} moveDefault={label} onDone={clearChosen} />
    </div>
  );
}
