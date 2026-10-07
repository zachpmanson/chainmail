import { Button } from "../ui/controls";
import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { $api, type ChainHit } from "../../lib/api/api";
import { usePrefs } from "../../lib/prefs/usePrefs";
import { useEscapeToClear } from "../../lib/inbox/selection";
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

const PAGE = 50;

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
  const urlLabel = useSearch({ from: "/" }).label;
  const urlAccountId = useSearch({ from: "/" }).accountId;
  const settings = $api.useQuery("get", "/v1/settings", {});
  const save = $api.useMutation("post", "/v1/settings", {
    onSuccess: () => {
      void settings.refetch();
    },
  });

  // "" in the URL is All mail chosen on purpose; only undefined falls back to the
  // default, and the list waits for settings (`enabled` below) to avoid a swap.
  const home = urlLabel === undefined && urlAccountId === undefined;
  const label = urlLabel !== undefined ? urlLabel : (settings.data?.defaultFolder ?? "");
  const accountId =
    urlAccountId !== undefined
      ? urlAccountId
      : home
        ? settings.data?.defaultFolderAccountId
        : undefined;
  // Compare the whole location: one folder name can exist in several accounts.
  const isDefault =
    (settings.data?.defaultFolder ?? "") === label &&
    (settings.data?.defaultFolderAccountId ?? "") === (accountId ?? "");
  const pickFolder = (name: string, pickedAccountId?: string) =>
    navigate({
      to: "/",
      search: (prev) => ({
        ...prev,
        label: name,
        accountId: pickedAccountId,
      }),
    });
  const makeDefault = (on: boolean) =>
    save.mutate({
      body: {
        defaultFolder: on ? label : "",
        defaultFolderAccountId: on ? (accountId ?? "") : "",
      },
    });
  // Choosing a thread dismisses compose, or the thread would open hidden beneath it.
  const openChain = (root: string) => {
    closeCompose();
    if (root === opened) return;
    navigate({ to: "/", search: (prev) => ({ ...prev, open: root }) });
  };
  const closeChain = () => navigate({ to: "/", search: (prev) => ({ ...prev, open: undefined }) });

  // pageParamName injects the cursor as `before`.
  const inbox = $api.useInfiniteQuery(
    "get",
    "/v1/search",
    {
      params: {
        query: { limit: PAGE, ...(label ? { label } : {}), ...(accountId ? { accountId } : {}) },
      },
    },
    {
      // Settled, not succeeded: a failed settings read means All mail, not waiting forever.
      enabled: !settings.isPending,
      pageParamName: "before",
      initialPageParam: "",
      getNextPageParam: (last, pages, cursor) => {
        const chains = last.chains ?? [];
        // A short page is the end; asking again would return the same page forever.
        if (chains.length < PAGE) return undefined;
        const oldest = chains[chains.length - 1];
        if (!oldest) return undefined;
        // A thread's `last` is its newest message, so a long thread can straddle the
        // cursor. Stop unless the page moves the cursor and adds threads, or paging loops.
        if (cursor && oldest.last >= cursor) return undefined;
        const shown = new Set(
          pages.slice(0, -1).flatMap((p) => (p.chains ?? []).map((c) => c.rootExtId)),
        );
        return chains.some((c) => !shown.has(c.rootExtId)) ? oldest.last : undefined;
      },
    },
  );

  // Straddling threads come back on both pages; dedupe, since rows key on root ext id.
  const seen = new Set<string>();
  const rows: ChainHit[] = [];
  for (const page of inbox.data?.pages ?? []) {
    for (const c of page.chains ?? []) {
      if (seen.has(c.rootExtId)) continue;
      seen.add(c.rootExtId);
      rows.push(c);
    }
  }

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
  const fetchNextPage = inbox.fetchNextPage;
  useEffect(() => {
    const marker = end.current;
    if (!marker || !paging) return;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) void fetchNextPage();
      },
      { rootMargin: "300px" },
    );
    io.observe(marker);
    return () => io.disconnect();
  }, [paging, fetchNextPage]);

  return (
    <div className="wrap ibwrap mx-0 w-full max-w-none px-0 pt-0 pb-0 min-[60rem]:flex min-[60rem]:flex-1 min-[60rem]:flex-col min-[60rem]:min-h-0">
      {inbox.isError && !inbox.data ? <Failure error={inbox.error} /> : null}
      {!inbox.isPending && !inbox.isError && rows.length === 0 ? (
        <p className="mt-2 flex-[1_1_100%] text-[.78rem] text-muted">
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
              currentAccountId={accountId}
              isDefault={isDefault}
              onPick={pickFolder}
              onDefault={makeDefault}
            />
            <div className="iblistwrap min-w-0 min-[60rem]:min-h-0 min-[60rem]:flex-1 min-[60rem]:overflow-y-auto min-[60rem]:rounded-lg min-[60rem]:border min-[60rem]:border-line min-[60rem]:bg-card min-[60rem]:[scrollbar-gutter:stable]">
              {compact && rows.length > 0 ? <CompactListHeader /> : null}
              {rows.length > 0 ? (
                <ul className="m-0 list-none divide-y divide-line overflow-hidden rounded-lg border border-line bg-card p-0 min-[60rem]:border-0 min-[60rem]:rounded-none min-[60rem]:bg-transparent">
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
                    <p className="m-0 flex-[1_1_100%] text-[.78rem] text-muted" role="status">
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
                    className="mx-auto mt-3 block rounded-full px-4 py-1.5 text-[.78rem] text-muted hover:border-accent hover:text-accent"
                    onClick={() => inbox.fetchNextPage()}
                  >
                    Try again
                  </Button>
                </>
              ) : null}
            </div>
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
