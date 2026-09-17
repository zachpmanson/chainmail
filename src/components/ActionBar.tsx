import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useQueryClient } from "@tanstack/react-query";
import { $api } from "../lib/api";
import { useBuildPage } from "../lib/build";
import { dropFromLists, putBackLists } from "../lib/lists";
import { dismissToast, pushToast } from "../lib/toasts";
import { Failure } from "./ThreadPreview";
import {
  ArchiveGlyph,
  MoveFolder,
  SAID_MS,
  TrashGlyph,
  VERBS,
  refusal,
  sentence,
  staleAfterMail,
} from "./MailVerbs";

/**
 * Where the bar is drawn: the slot the site header leaves in its own row.
 *
 * Looked up rather than handed down. The bar is not a panel at the foot of the
 * page any more: it takes the nav's row while a selection stands — see the `:has`
 * rule in styles.css — because the reader has ticked some rows and the only thing
 * the top of the page then needs to say is what can be done with them. The slot
 * is the shell's, the selection is the page's, and the pages draw this bar.
 *
 * The read is per render and not memoised, because the header is committed in the
 * same commit as the first render of the page below it: on that render there is
 * no slot in the document yet, and a value captured then would be null forever.
 * Nothing renders the bar before that — it appears when a thread is ticked, which
 * is a later render by construction.
 *
 * Null where there is no header at all: the static pages scripts/render.tsx ships
 * have no nav and no bar. The bar is then drawn where it stands, which is the
 * fallback for a page with no slot rather than a second layout.
 */
function buildBarSlot(): HTMLElement | null {
  return typeof document === "undefined" ? null : document.querySelector(".buildslot");
}

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
 * The reader's own addresses are **not** here either, and not only because the
 * bar has no room for them: they are a setting, not a field about the page being
 * braided. They decide which messages are marked as the reader's wherever mail is
 * read, including threads nobody ever braided a page from, so they are written on
 * the services page with the other settings and read from there by whoever needs
 * them — here, and the reading pane.
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
   * What to do once the ticked chains are no longer the ones to act on, whether
   * because a mail action took them out of the list or because the reader said
   * "Deselect all". A mail action's chains are gone — a message that has been
   * archived, deleted or moved is not in the inbox any more — so leaving the
   * boxes ticked would invite a second action on threads that are already gone.
   */
  onDone: () => void;
}) {
  const [title, setTitle] = useState("");
  const [braiding, setBraiding] = useState(false);
  // What the last action here had to say is drawn in the shell's corner, not in
  // the bar (see Toasts): the bar has to be able to leave as soon as the ticks
  // are cleared, and a sentence that kept it on the page would hold the header's
  // row for a claim about work that is already over. The id is kept so that a
  // later action replaces its own sentence rather than stacking a second one.
  const said = useRef<number | null>(null);
  const say = (text: string, kind: "note" | "fail") => {
    if (said.current !== null) dismissToast(said.current);
    said.current = pushToast(text, kind, kind === "note" ? SAID_MS : null);
  };
  const { build, start } = useBuildPage();
  // The reader's addresses, read where they are resolved. The braid needs them to
  // mark the reader's own messages as theirs; nothing in the corpus records which
  // mailbox it was collected from, so they can only be told, never inferred — and
  // the setting tells it as a person, which the server reads back out as that
  // person's mailboxes, so a page braided today marks the aliases the corpus knows
  // today rather than the ones the reader had written down.
  const settings = $api.useQuery("get", "/v1/settings", {});
  // The folders a move can name, which are the mailbox's own: the dropdown offers
  // what /v1/labels serves rather than a list this page keeps, so a folder created
  // in the mail app a minute ago is offered here without anything being synced.
  const folders = $api.useQuery("get", "/v1/labels", {});
  const qc = useQueryClient();
  // The header's slot, or null on a page that has no header.
  const slot = buildBarSlot();

  // The ticked chains are filed away as the button is pressed, out of the folder
  // view they were listed in (see lib/lists): a reader who archives six threads
  // means them to be gone from the list they are looking at, and waiting for the
  // mailbox left six rows sitting there under a sentence saying they had moved. A
  // refusal puts them back — and the sentence is written from the server's own
  // answer either way, so what is claimed afterwards is never what was assumed.
  const act = $api.useMutation("post", "/v1/mail", {
    onMutate: (v) => ({ was: dropFromLists(qc, v.body.chains) }),
    onSuccess: (res) => {
      say(sentence(res.action, res.labels, res.changed, res.skipped), "note");
      onDone();
      staleAfterMail(qc);
    },
    onError: (e: unknown, _v, ctx) => {
      if (ctx) putBackLists(qc, ctx.was);
      // Said in the same words the pane uses for the same write, and in the same
      // place: the server's own answer, in the corner. Nothing is filed as a report
      // of work that did happen — a refusal names the verb that did not run.
      say(refusal(e, "-mail-write", VERBS[_v.body.action] ?? "That change"), "fail");
    },
  });

  // The clock and the way out are the store's now, so nothing here waits on a
  // sentence or keeps one alive past the ticks it was about: the bar is on the
  // page because there is a selection to act on, and for no other reason.
  if (chosen.length === 0) return null;

  const busy = act.isPending;
  // Folders, without the inbox: a move that named INBOX would be a move whose
  // destination is the place it is leaving. Sorted by name because a dropdown is
  // read by looking for a word, unlike the folder list beside the mail, which is
  // ordered by how much is in each.
  const moves = (folders.data?.labels ?? [])
    .map((f) => f.name)
    .filter((name) => name !== "INBOX")
    .sort((a, b) => a.localeCompare(b));

  const body = (
    <>
      {chosen.length > 0 ? (
        <div className="ibbuild">
          <button type="button" onClick={() => setBraiding(true)}>
            Braid Threads
          </button>
          {/* The two mailbox verbs are glyphs. The bar also holds a braid, a
              folder dropdown, the count and the way out, and spelling Archive
              and Delete along that row is what wrapped it on a laptop. The word
              is still on the button — it is the tooltip, and what a screen
              reader reads — but the row draws the box and the bin. */}
          <button
            type="button"
            className="ibicon"
            aria-label="Archive"
            title="Archive"
            disabled={busy}
            onClick={() => act.mutate({ body: { chains: chosen, action: "archive" } })}
          >
            <ArchiveGlyph />
          </button>
          <button
            type="button"
            className="ibicon"
            aria-label="Delete"
            title="Delete"
            disabled={busy}
            onClick={() => act.mutate({ body: { chains: chosen, action: "trash" } })}
          >
            <TrashGlyph />
          </button>
          {/* One control for the move, and one decision in it. It used to be a
              labelled field with a Move button beside it, which is the reader
              being asked to say the same thing twice: they know the folder when
              they reach for the dropdown, and naming it again is a second act
              with no second thought behind it. So the choice is the action, and
              the control goes on showing nothing but its own glyph: what it did is
              not a state of the mailbox this bar can hold — the mail has gone, and
              the sentence below says where (see MoveFolder for why the control is
              an icon button with the real dropdown over it).

              The empty option is the placeholder and is the state the control
              stays in: a select whose value never moves needs no state of its
              own, and a folder named in it would be a folder the reader could
              pick a second time by accident. */}
          <MoveFolder
            folders={moves}
            busy={busy}
            onMove={(to) => act.mutate({ body: { chains: chosen, action: "move", labels: [to] } })}
          />
          {/* What the bar is about, and the way out of it, together at the far
              end: the count is the only thing on the row that says how much is
              ticked, and deselecting is the one action that is not about the
              mail. */}
          <span className="ibright">
            <span className="ibselcount">
              {chosen.length} selected
            </span>
            <button type="button" className="ibclear" onClick={onDone}>
              Deselect all
            </button>
          </span>
        </div>
      ) : null}

      {/* What happened is drawn in the shell's corner (see Toasts): which verb, how
          many messages, and the one number that outlives the action — what is in
          the trash can be got back, and how long that lasts is the whole reason
          Delete is not frightening. Nothing about it is a line of this bar, which
          is here for the ticks and leaves with them. */}

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
    </>
  );

  // Portalled into the header where there is one, so the bar replaces the nav
  // rather than sitting above the page's own controls; drawn in place otherwise.
  return slot ? createPortal(body, slot) : body;
}

/**
 * The braid dialog: a title, and the button that asks the service for the page.
 *
 * A modal rather than a field in the bar, because a title is a decision about the
 * page — the file it is saved as, and the heading it carries — and it belongs
 * beside the button that commits to it. In the bar it competed for the same line
 * as the two mailbox verbs, which is a line about a different set of decisions.
 *
 * The field is optional and its default is stated: the service borrows the
 * earliest ticked thread's subject when none is given, which is right far more
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
  // Escape closes the dialog, matching the thread preview's habits; the listener
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
            {count} thread{count === 1 ? "" : "s"} ticked
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
          Left empty, the page is titled with the earliest thread's subject.
        </p>
        {/* Seconds of silence reads as a broken page, so the wait says what it is
            waiting on and how much of it there is. */}
        {busy ? (
          <p className="selnote" role="status">
            Recovering HTML and detecting boilerplate across {count} thread
            {count === 1 ? "" : "s"}. This takes a few seconds.
          </p>
        ) : null}
        {error ? <Failure error={error} /> : null}
      </div>
    </div>
  );
}
