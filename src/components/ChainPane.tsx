import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError, $api } from "../lib/api";
import { ChainMessages } from "./ChainMessages";
import type { PreviewableChain } from "./ChainPreview";

/**
 * The reading pane: the chain a list has open, read at the right of it.
 *
 * One pane for both pages. The search page drew a candidate as its own cards
 * (sender, day, plain text), so the same conversation looked like different
 * software depending on whether you had browsed to it or searched for it — the
 * complaint the pane's own renderer already answered once
 * (`spec.RenderBodies`, `ChainMessages`), and the answer is the same here: the
 * transcript's own bubbles, over a corpus read that carries each body rendered.
 *
 * What each page keeps is where the pane goes back to ("← List" and "← Results"
 * — CSS-hidden where both panels fit) and what it says with nothing open, since
 * those are facts about the page rather than about the chain.
 */

export function ChainPane({
  chain,
  label,
  backLabel,
  empty,
  onClose,
}: {
  /** The chain to read, or null when the page has nothing to show yet. A caller
   *  holding only a root ext id is enough: the chain is fetched by id, and the
   *  head then claims no subject and no count rather than inventing them. */
  chain: PreviewableChain | null;
  /** The pane's accessible name, which is the page's word for what is in it. */
  label: string;
  backLabel: string;
  empty: string;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();

  // The read-state write: the one thing a list changes in the mailbox itself, and
  // the reason the unread counts it draws mean what the reader's phone means.
  //
  // The chain's own count is the state the button acts on, and the list is what
  // holds it, so the write invalidates the list rather than patching a row in
  // place: the server reconciles every message in the chain, and the number it
  // comes back with is the mailbox's answer about all of them — a client that set
  // `unread: 0` by hand would be claiming something it had not been told. Both
  // pages ask that question of the same endpoint, so one invalidation serves
  // either of them.
  const [note, setNote] = useState<string | null>(null);
  const read = $api.useMutation("post", "/v1/read", {
    onSuccess: () => {
      setNote(null);
      void queryClient.invalidateQueries({ queryKey: ["get", "/v1/search"] });
    },
    onError: (e: unknown) => {
      // Said in the pane, not only in the console, and the disabled case in words
      // a reader can act on: a host started without -mark-read refuses every
      // press, and a silent button would read as a broken one.
      setNote(
        e instanceof ApiError && e.status === 403
          ? "This host cannot change the mailbox: it was started without -mark-read."
          : `Marking failed: ${e instanceof Error ? e.message : String(e)}`,
      );
    },
  });

  return (
    <aside className="ibread" aria-label={label}>
      {chain ? (
        <>
          <div className="ibread-head">
            <button type="button" className="ibback" onClick={onClose}>
              {backLabel}
            </button>
            <span className="ibread-subj">{chain.subject || "(no subject)"}</span>
            <span className="note">
              {chain.entries
                ? `${chain.entries} entr${chain.entries === 1 ? "y" : "ies"}`
                : ""}
            </span>
            {/* The read-state control, and the only explicit one: nothing is
                marked by looking at it. A pane opens the top of the list by
                itself, so a mark-on-open rule would clear the badge for mail
                nobody has read — and undoing that is a second click nobody knows
                to make. Absent when the caller holds only an id, because then the
                state is unknown and the button could only guess which way it
                goes.

                Rightmost in the strip, past the count, so it is one place to
                reach for on every chain. The circle *is* the state — filled for
                unread, an outline for read, the same grammar the row badge uses —
                and the press is the other one: no label, because a word here
                would be a second thing to read on a line whose subject is the
                thing being read. */}
            {chain.unread !== undefined ? (
              <button
                type="button"
                className={`ibread-read${chain.unread > 0 ? " unread" : ""}`}
                disabled={read.isPending}
                aria-label={chain.unread > 0 ? "Mark read" : "Mark unread"}
                aria-pressed={chain.unread > 0}
                title={
                  chain.unread > 0
                    ? "Unread in the mailbox — mark this chain read"
                    : "Read in the mailbox — mark this chain unread"
                }
                onClick={() =>
                  read.mutate({
                    body: { chain: chain.rootExtId, unread: chain.unread === 0 },
                  })
                }
              />
            ) : null}
          </div>
          {note ? (
            <p className="selnote" role="status">
              {note}
            </p>
          ) : null}
          {/* The same component the page built from this thread uses, over a
              corpus read that carries each body already rendered. */}
          <ChainMessages chain={chain} />
        </>
      ) : (
        <p className="selnote">{empty}</p>
      )}
    </aside>
  );
}