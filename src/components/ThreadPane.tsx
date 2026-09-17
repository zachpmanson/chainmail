import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { $api } from "../lib/api";
import { dropFromLists, markInLists, putBackLists } from "../lib/lists";
import { dismissToast, pushToast } from "../lib/toasts";
import { ThreadMessages } from "./ThreadMessages";
import { AttachmentCount, MailCount, PeopleCount } from "./ThreadRow";
import {
  MoveFolder,
  ArchiveGlyph,
  SAID_MS,
  TrashGlyph,
  VERBS,
  refusal,
  sentence,
  staleAfterMail,
} from "./MailVerbs";
import type { PreviewableThread } from "./ThreadPreview";

/**
 * The reading pane: the thread a list has open, read at the right of it.
 *
 * One pane for both pages. The search page drew a candidate as its own cards
 * (sender, day, plain text), so the same conversation looked like different
 * software depending on whether you had browsed to it or searched for it — the
 * complaint the pane's own renderer already answered once
 * (`spec.RenderBodies`, `ThreadMessages`), and the answer is the same here: the
 * transcript's own bubbles, over a corpus read that carries each body rendered.
 *
 * What each page keeps is where the pane goes back to ("← List" and "← Results"
 * — CSS-hidden where both panels fit) and what it says with nothing open, since
 * those are facts about the page rather than about the thread.
 *
 * The pane is also where the two mailbox verbs live for the thread that is open
 * (see MailVerbs): archiving the thread you have just read is the commonest way of
 * finishing with it, and reaching for it should not mean ticking the row behind
 * the pane you are reading.
 */

export function ThreadPane({
  thread,
  label,
  backLabel,
  empty,
  onClose,
}: {
  /** The thread to read, or null when the page has nothing to show yet. A caller
   *  holding only a root ext id is enough: the thread is fetched by id, and the
   *  head then claims no subject and no count rather than inventing them. */
  thread: PreviewableThread | null;
  /** The pane's accessible name, which is the page's word for what is in it. */
  label: string;
  backLabel: string;
  empty: string;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();

  // What the last write here has to say is drawn in the shell's corner, not in
  // this pane (see Toasts): an account of work that is over must not take a row
  // from the thread being read. What the pane keeps is the id of its own
  // notification, because an account of what happened to one thread is a claim
  // about a thread the reader may have left — the pane is not remounted between
  // threads, only its contents change — so it is taken down by hand when the
  // thread changes rather than left to its clock.
  const said = useRef<number | null>(null);
  const say = (text: string, kind: "note" | "fail") => {
    if (said.current !== null) dismissToast(said.current);
    said.current = pushToast(text, kind, kind === "note" ? SAID_MS : null);
  };

  useEffect(() => {
    if (said.current !== null) dismissToast(said.current);
    said.current = null;
  }, [thread?.rootExtId]);

  // The read-state write: the one thing a list changes in the mailbox itself, and
  // the reason the unread counts it draws mean what the reader's phone means.
  //
  // The thread's own count is the state the button acts on, and the list is what
  // holds it, so the write changes the list as it is pressed (see lib/lists) and
  // then re-reads it: the server reconciles every message in the thread, and the
  // number it comes back with is the mailbox's answer about all of them — a client
  // that ended there would be claiming something it had not been told. Both pages
  // ask that question of the same endpoint, so one invalidation serves either of
  // them, and a refusal puts the count back rather than leaving a mark the mailbox
  // does not agree with.
  const read = $api.useMutation("post", "/v1/read", {
    onMutate: (v) => ({ was: markInLists(queryClient, v.body.chain, v.body.unread) }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["get", "/v1/search"] });
    },
    onError: (e: unknown, _v, ctx) => {
      // Said in the pane, not only in the console, and the disabled case in words
      // a reader can act on: a host started without -mark-read refuses every
      // press, and a silent button would read as a broken one.
      if (ctx) putBackLists(queryClient, ctx.was);
      say(refusal(e, "-mark-read", "Marking"), "fail");
    },
  });

  // The folders a move can name: the mailbox's own list, without the inbox — a move
  // whose destination is the folder it is leaving — and sorted, because a dropdown
  // is read by looking for a word. The same list, filtered the same way and in the
  // same order as the selection bar's move control, so the two cannot offer a reader
  // different folders.
  const folders = $api.useQuery("get", "/v1/labels", {});
  const moves = (folders.data?.labels ?? [])
    .map((f) => f.name)
    .filter((name) => name !== "INBOX")
    .sort((a, b) => a.localeCompare(b));

  // The two mailbox verbs, on the one thread this pane has open: the same pair the
  // bar draws for threads a reader has ticked, doing the same thing to a set of
  // one. Nothing is confirmed first — the sentence says what happened and that the
  // trash keeps it for 30 days, which is the account the bar gives for the same
  // press.
  //
  // The row goes as it is pressed, out of the folder view it was listed in (see
  // lib/lists): archiving a thread the reader is looking at is answered by it not
  // being there any more, and waiting for the mailbox made the bin look like a
  // button that had missed. A refusal puts it back and says so in the same breath.
  //
  // A write here does not tick or untick anything, so the bar has nothing to say
  // about it: the account belongs to the surface the reader pressed.
  const act = $api.useMutation("post", "/v1/mail", {
    onMutate: (v) => ({ was: dropFromLists(queryClient, v.body.chains) }),
    onSuccess: (res) => {
      say(sentence(res.action, res.labels, res.changed, res.skipped), "note");
      staleAfterMail(queryClient);
    },
    onError: (e: unknown, v, ctx) => {
      if (ctx) putBackLists(queryClient, ctx.was);
      say(refusal(e, "-mail-write", VERBS[v.body.action] ?? "That change"), "fail");
    },
  });

  return (
    <aside className="ibread" aria-label={label}>
      {thread ? (
        <>
          <div className="ibread-head">
            <button type="button" className="ibback" onClick={onClose}>
              {backLabel}
            </button>
            <span className="ibread-subj">{thread.subject || "(no subject)"}</span>
            {/* How much mail is in the thread, how many people, and how many files
                — worn the way the row that opened it wears them: the same glyphs,
                the same numbers, the same classes. The head used to say "4
                entries" in words, which made the pane and the list describe one
                thread in two vocabularies — and it is the same reader, a moment
                later. Where the row draws a count only when it is worth the room
                (see ThreadRow), the head draws the paperclip at zero too: this is
                the thread the reader has open, and "nothing attached" is an answer
                about it. */}
            <span className="ibread-counts">
              {thread.people !== undefined ? <PeopleCount people={thread.people} /> : null}
              {thread.entries !== undefined ? <MailCount entries={thread.entries} /> : null}
              {thread.attachments !== undefined ? (
                <AttachmentCount attachments={thread.attachments} />
              ) : null}
            </span>
            {/* The two mailbox verbs, beside the read circle and on the thread
                that is open rather than on a ticked set. Glyphs, and the same two
                the bar draws (see MailVerbs): the strip's line is the subject, and
                two words here would be two more things to read on it — the word is
                still the button's name and its tooltip.

                Before the circle rather than past it, so the read control keeps
                the end of the row on every thread, ticked or not, and so Delete is
                not the outermost thing under the pointer. */}
            {/* Move, drawn as the selection bar draws it: one dropdown, and the
                glyph the verbs beside it are drawn as (see MoveFolder). */}
            <MoveFolder
              folders={moves}
              busy={act.isPending || folders.isPending}
              onMove={(to) =>
                act.mutate({ body: { chains: [thread.rootExtId], action: "move", labels: [to] } })
              }
            />
            <button
              type="button"
              className="ibicon"
              aria-label="Archive"
              title="Archive"
              disabled={act.isPending}
              onClick={() =>
                act.mutate({ body: { chains: [thread.rootExtId], action: "archive" } })
              }
            >
              <ArchiveGlyph />
            </button>
            <button
              type="button"
              className="ibicon"
              aria-label="Delete"
              title="Delete"
              disabled={act.isPending}
              onClick={() => act.mutate({ body: { chains: [thread.rootExtId], action: "trash" } })}
            >
              <TrashGlyph />
            </button>
            {/* The read-state control, and the only explicit one: nothing is
                marked by looking at it. A pane opens the top of the list by
                itself, so a mark-on-open rule would clear the badge for mail
                nobody has read — and undoing that is a second click nobody knows
                to make. Absent when the caller holds only an id, because then the
                state is unknown and the button could only guess which way it
                goes.

                Rightmost in the strip, past the count and past the two verbs, so
                it is one place to reach for on every thread. It is the same button
                as the two verbs beside it — same box, same glyph size, same hover
                (see .ibread-read) — because a circle floating on the line reads as
                a mark rather than as something to press, and this is the control
                pressed most in this pane. The circle *is* the state, filled for
                unread and an outline for read, and the press is the other one: no
                label, because a word here would be a second thing to read on a
                line whose subject is the thing being read. The two states are the
                same grey — the shape is the whole of the difference, since a
                second colour on one of three verbs on the line would read as
                emphasis rather than as a state. */}
            {thread.unread !== undefined ? (
              <button
                type="button"
                className={`ibicon ibread-read${thread.unread > 0 ? " unread" : ""}`}
                disabled={read.isPending}
                aria-label={thread.unread > 0 ? "Mark read" : "Mark unread"}
                aria-pressed={thread.unread > 0}
                title={
                  thread.unread > 0
                    ? "Unread in the mailbox — mark this thread read"
                    : "Read in the mailbox — mark this thread unread"
                }
                onClick={() =>
                  read.mutate({
                    body: { chain: thread.rootExtId, unread: thread.unread === 0 },
                  })
                }
              >
                {/* One circle, and the state is how it is drawn: a filled disc for
                    unread, an outline for read. Drawn as a glyph rather than as a
                    bordered box so that all three controls in this strip are the
                    same kind of thing — the archive bin and the trash can are
                    glyphs in the same button, and the read state is a mark, not a
                    fourth shape of control. */}
                <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
                  <circle cx="8" cy="8" r="5.2" />
                </svg>
              </button>
            ) : null}
          </div>
          {/* The same component the page built from this thread uses, over a
              corpus read that carries each body already rendered — in the pane's
              own scroll box, so the head above is a line of the pane rather than
              the first thing in the thread (see .ibreadwrap). */}
          <div className="ibreadwrap">
            <ThreadMessages thread={thread} />
          </div>
        </>
      ) : (
        <p className="selnote">{empty}</p>
      )}
    </aside>
  );
}
