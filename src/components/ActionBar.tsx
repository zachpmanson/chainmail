import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { $api } from "../lib/api";
import { useBuildPage } from "../lib/build";
import { Failure } from "./ChainPreview";

/**
 * The bar of things a reader can do to the chains they ticked: braid them into a
 * page, or move them out of the inbox.
 *
 * One bar for both pages. It was written twice — at the foot of the inbox and at
 * the foot of the search results — and the two copies had already drifted: the
 * inbox's recorded the reader's addresses as a preference on the way past, the
 * search page's used them once and dropped them, so naming yourself while
 * building from the search left the inbox pane refusing to mark your own mail.
 * Which list the chains were ticked in is not a fact about what the bar does; the
 * bar appears once something is ticked, on both pages, and the rules about what
 * happens to the ticked set live in one place.
 *
 * Building and moving are the same kind of decision from the reader's side — they
 * ticked some threads and now something happens to all of them — so they are the
 * same bar, even though one writes a file and the other writes the mailbox. What
 * is *not* here is a second copy of either: the braid asks the service through
 * `useBuildPage`, exactly as the page route does, and the three mail actions are
 * one call to one endpoint that spells what each action means in the mailbox's
 * own vocabulary.
 *
 * The reader's own addresses are **not** here either. They are a setting, not a
 * field about the page being braided: they decide which messages are marked as
 * the reader's wherever mail is read, including threads nobody ever braided a
 * page from. They are written on the services page, with the other settings, and
 * read from there by whoever needs them — here, and the reading pane.
 */
export function ActionBar({
  chosen,
  queries,
  onDone,
}: {
  /** Root ext ids of the chains to act on, in the order they were ticked. */
  chosen: string[];
  /**
   * The searches to record on the page, when a search is what found the chains,
   * so a later refresh can propose what the same query would find now. The inbox
   * passes none: no query found its chains, and a made-up one would have refresh
   * proposing threads nobody asked about.
   */
  queries?: { q: string; note?: string }[];
  /**
   * What to do once the ticked chains are no longer in the list they were ticked
   * in. A mail action takes them out of it — a message that has been archived,
   * deleted or moved is not in the inbox any more — so leaving the boxes ticked
   * would invite a second action on threads that are already gone.
   */
  onDone: () => void;
}) {
  const [title, setTitle] = useState("");
  const [braiding, setBraiding] = useState(false);
  const [folder, setFolder] = useState("");
  const [said, setSaid] = useState("");
  const { build, start } = useBuildPage();
  // The reader's addresses, read where they are written. The braid needs them to
  // mark the reader's own messages as theirs; nothing in the corpus records which
  // mailbox it was collected from, so they can only be told, never inferred.
  const settings = $api.useQuery("get", "/v1/settings", {});
  // The folders a move can name, which are the mailbox's own: the dropdown offers
  // what /v1/labels serves rather than a list this page keeps, so a folder created
  // in the mail app a minute ago is offered here without anything being synced.
  const folders = $api.useQuery("get", "/v1/labels", {});
  const qc = useQueryClient();

  const act = $api.useMutation("post", "/v1/mail", {
    onSuccess: (res) => {
      setSaid(sentence(res.action, res.labels, res.changed, res.skipped));
      setFolder("");
      onDone();
      // The mail moved, so everything that counts or lists mail is now stale:
      // the list the reader is looking at, the folder counts, the corpus totals,
      // and the open thread's own labels.
      void qc.invalidateQueries({ queryKey: ["get", "/v1/search"] });
      void qc.invalidateQueries({ queryKey: ["get", "/v1/labels"] });
      void qc.invalidateQueries({ queryKey: ["get", "/v1/stats"] });
      void qc.invalidateQueries({ queryKey: ["get", "/v1/chains/{rootExtId}"] });
    },
  });

  // Nothing ticked is nothing to do anything to, so there is no bar: a form with
  // no object is an instruction to do something with nothing.
  //
  // Except for the sentence an action leaves behind. The ticks are cleared when
  // the mail moves — the chains are not in this list any more — so a bar that
  // vanished with them would take the only account of what happened with it, and
  // the reader who just moved four threads would be shown a list that quietly
  // changed. The note stands alone until something is ticked again, at which
  // point it is about work that is over and is dropped.
  const note = chosen.length === 0 ? said : "";
  if (chosen.length === 0 && !note) return null;

  const busy = act.isPending;
  // Folders, without the inbox: a move that named INBOX would be a move whose
  // destination is the place it is leaving. Sorted by name because a dropdown is
  // read by looking for a word, unlike the folder list beside the mail, which is
  // ordered by how much is in each.
  const moves = (folders.data?.labels ?? [])
    .map((f) => f.name)
    .filter((name) => name !== "INBOX")
    .sort((a, b) => a.localeCompare(b));

  return (
    <div className="selbuild ibbuild">
      {chosen.length > 0 ? (
        <>
          <button type="button" onClick={() => setBraiding(true)}>
            Braid Threads
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => act.mutate({ body: { chains: chosen, action: "archive" } })}
          >
            Archive
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => act.mutate({ body: { chains: chosen, action: "trash" } })}
          >
            Delete
          </button>
          {/* A div rather than a label, unlike the title field: the select names
              itself (aria-label), and a wrapping label would give it a second
              name made of its own options' text. */}
          <div className="self ibmove">
            <span>Move to</span>
            <select
              value={folder}
              disabled={busy}
              onChange={(e) => setFolder(e.target.value)}
              aria-label="Move to"
            >
              {/* The empty choice is the state the control starts in, and it is
                  what keeps Move disabled: a folder is named before mail is
                  moved into it, never after. */}
              <option value="">choose a folder…</option>
              {moves.map((name) => (
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
            </select>
          </div>
          <button
            type="button"
            disabled={busy || !folder}
            onClick={() =>
              act.mutate({ body: { chains: chosen, action: "move", labels: [folder] } })
            }
          >
            Move
          </button>
        </>
      ) : null}

      {/* What happened, in the reader's own terms: which verb, how many messages,
          and the one number that outlives the action — what is in the trash can
          be got back, and how long that lasts is the whole reason Delete is not
          frightening. */}
      {note ? (
        <p className="selnote" role="status">
          {note}
        </p>
      ) : null}
      {act.isError ? <Failure error={act.error} /> : null}

      {braiding ? (
        <BraidDialog
          count={chosen.length}
          title={title}
          onTitle={setTitle}
          busy={build.isPending}
          error={build.isError ? build.error : null}
          onClose={() => setBraiding(false)}
          onBraid={() =>
            start({ chains: chosen, title, me: settings.data?.me ?? [], queries })
          }
        />
      ) : null}
    </div>
  );
}

/**
 * What to tell the reader after a mail action, built from what the service
 * answered rather than from what was asked for: the counts are the mailbox's.
 *
 * The skipped count is named only when there is one, because it is the exception
 * — an entry that survived only inside somebody's quote, or a Slack post, has no
 * mailbox copy to move. It is not a failure and it is not silent either: a reader
 * who moved four threads and is told about three messages needs to know why the
 * number is not what they can see.
 */
function sentence(
  action: string,
  labels: string[] | undefined,
  changed: number,
  skipped: number,
): string {
  const messages = `${changed} message${changed === 1 ? "" : "s"}`;
  const what =
    action === "move"
      ? `Moved ${messages} to ${labels?.join(", ") ?? ""}`
      : action === "trash"
        ? `Deleted ${messages} — in the trash, recoverable for 30 days`
        : `Archived ${messages} — out of the inbox, still in All Mail`;
  return skipped > 0
    ? `${what}. ${
        skipped === 1
          ? "1 entry has no mailbox copy and was left alone"
          : `${skipped} entries have no mailbox copy and were left alone`
      }.`
    : `${what}.`;
}

/**
 * The braid dialog: a title, and the button that asks the service for the page.
 *
 * A modal rather than a field in the bar, because a title is a decision about the
 * page — the file it is saved as, and the heading it carries — and it belongs
 * beside the button that commits to it. In the bar it competed for the same line
 * as Archive and Delete, which is a line about the mailbox.
 *
 * The field is optional and its default is stated: the service borrows the
 * earliest ticked chain's subject when none is given, which is right far more
 * often than a name a second field could invent.
 */
function BraidDialog({
  count,
  title,
  onTitle,
  busy,
  error,
  onBraid,
  onClose,
}: {
  count: number;
  title: string;
  onTitle: (t: string) => void;
  busy: boolean;
  error: unknown;
  onBraid: () => void;
  onClose: () => void;
}) {
  // Escape closes the dialog, matching the chain preview's habits; the listener
  // lives here because the dialog only exists while it is open.
  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="selpv"
      role="dialog"
      aria-modal="true"
      aria-label="Braid threads"
      onClick={onClose}
    >
      <div className="selpv-panel" onClick={(e) => e.stopPropagation()}>
        <div className="selpv-head">
          <b>braid threads</b>
          <span className="note">
            {count} chain{count === 1 ? "" : "s"} ticked
          </span>
          <button type="button" className="selpv-close" onClick={onClose}>
            Close
          </button>
        </div>
        <div className="selform braidform">
          <label className="self">
            <span>Page title</span>
            <input
              autoFocus
              value={title}
              onChange={(e) => onTitle(e.target.value)}
              placeholder="optional"
            />
          </label>
          <button type="button" disabled={busy} onClick={onBraid}>
            {busy ? "Braiding…" : "Braid"}
          </button>
        </div>
        <p className="selnote">
          Left empty, the page is titled with the earliest chain's subject.
        </p>
        {/* Seconds of silence reads as a broken page, so the wait says what it is
            waiting on and how much of it there is. */}
        {busy ? (
          <p className="selnote" role="status">
            Recovering HTML and detecting boilerplate across {count} chain
            {count === 1 ? "" : "s"}. This takes a few seconds.
          </p>
        ) : null}
        {error ? <Failure error={error} /> : null}
      </div>
    </div>
  );
}
