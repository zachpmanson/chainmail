/**
 * Reddit-style threading: the replies to a message drawn as a tree under it,
 * each level indented by how deep in the reply graph it is.
 *
 * A transcript is chronological, which is right for reading a conversation that
 * happened in time and wrong for reading one that forked: in a trail where three
 * people each answered the same question and one of them was answered again, the
 * flat order puts the answer to the answer under the next person's answer to
 * something else, and the reader has to hold the graph in their head to see who
 * was talking to whom. The tree is the same graph drawn instead of remembered —
 * the reply arrow each bubble already carries (`ReplyLink`) is a link to a
 * message *above* rather than to one further up, and the containers a message's
 * replies are drawn in are how far up it is.
 *
 * The order within a level is the transcript's own, and the clock still runs
 * down the page wherever the graph allows it: an answer is drawn after the
 * message it answers because it *was* sent after it, and only the siblings the
 * tree forces apart move. Nothing else about a bubble changes — the same
 * `Message`, the same reply link, the same anchoring — so the two views differ
 * by the containers the bubbles sit in and nothing about the bubbles themselves.
 *
 * Kept in the browser, like the list's width (see lib/panelWidth) and for the
 * same reason: which way a reader likes a thread drawn is about the reader and
 * not about the mailbox, so it is nothing the server should be told. It is a
 * fact about the window rather than about the account, which is also why it is
 * not a setting — and a browser that refuses to store it still gets the switch,
 * it just forgets between visits.
 */

/** One message in the drawn tree, and the replies it heads.
 *
 *  A tree rather than a list with a depth on it, because that is what the pane
 *  draws: each message's replies go in one container of their own, and that
 *  container's own border is the line beside them (see .ibread .stream .replies).
 *  How deep a message is, is how many of those containers it is inside. */
export interface Knot<E> {
  entry: E;
  replies: Knot<E>[];
}

/** The entries as a reply forest: one tree per opener, each message followed by
 *  the replies to it, each of those followed by the replies to it, and so on.
 *
 *  `key` is the entry's own handle and `parent` the handle of the message it
 *  answers, in whatever shape the caller holds them — this is the whole of what
 *  the function knows about a mail, so it is the same function over the pane's
 *  corpus entries and over anything else that has a reply graph.
 *
 *  Two facts about a real thread this has to survive rather than assume away:
 *
 *  - A parent that is not among these entries is not a missing parent. `/v1/chains`
 *    answers a thread whole, so a reply whose parent is absent is a message the
 *    corpus does not hold — the same case `replyOf` draws no arrow for — and it
 *    opens a tree rather than hanging off one that is not there.
 *  - A cycle is not possible in a mailbox and is not impossible in a corpus that
 *    reconstructs one, so the walk carries the entries it has already drawn and
 *    any it could not reach open trees of their own, in the transcript's order,
 *    at the end. Carrying what it has drawn is also what keeps a cycle from
 *    being a tree with no bottom: the walk is the only thing standing between a
 *    malformed graph and a component that recurses until the pane dies. A
 *    message that cannot be placed still has to be readable: dropping it would
 *    be the one failure a reader would not forgive this switch for.
 */
export function tree<E>(
  entries: E[],
  key: (e: E) => string,
  parent: (e: E) => string | undefined,
): Knot<E>[] {
  const known = new Set(entries.map(key));
  const children = new Map<string, E[]>();
  const roots: E[] = [];
  for (const e of entries) {
    const p = parent(e);
    // A message that answers itself is a cycle of one, and is drawn as an opener
    // like any other unplaceable mail — through the same branch, since there is
    // no tree to hang it off either.
    if (p === undefined || p === "" || p === key(e) || !known.has(p)) roots.push(e);
    else {
      const siblings = children.get(p);
      if (siblings) siblings.push(e);
      else children.set(p, [e]);
    }
  }

  const out: Knot<E>[] = [];
  const drawn = new Set<string>();
  const walk = (e: E, into: Knot<E>[]) => {
    const k = key(e);
    if (drawn.has(k)) return;
    drawn.add(k);
    const knot: Knot<E> = { entry: e, replies: [] };
    into.push(knot);
    for (const reply of children.get(k) ?? []) walk(reply, knot.replies);
  };
  for (const root of roots) walk(root, out);
  // Anything left is in a cycle: reachable from no opener, and so unreachable
  // from here except by starting somewhere inside it.
  for (const e of entries) if (!drawn.has(key(e))) walk(e, out);

  return out;
}

/** The switch's own key. Not `cm-tree`, which the app already spends on the reply
 *  tree's *layout* in the page view (horizontal, vertical or off — see
 *  client/behaviour and lib/panelWidth): the two are different questions about the
 *  same graph, and a reader who wants one does not necessarily want the other. */
const KEY = "cm-treeview";

/** Whether the reader wants the thread drawn as a tree. Off to begin with: the
 *  transcript's own order is what a thread has always been drawn in here, and a
 *  switch that defaults to on would be this build deciding a reader's preference
 *  for them. */
export function readTree(): boolean {
  try {
    return localStorage.getItem(KEY) === "1";
  } catch {
    return false;
  }
}

/** Remember the switch, or forget it — "0" is as good as absent, and is written
 *  rather than removed so that a reader who turned it off is not a reader who
 *  has never chosen. */
export function rememberTree(on: boolean): void {
  try {
    localStorage.setItem(KEY, on ? "1" : "0");
  } catch {
    /* private mode: the switch still works, the choice just does not last */
  }
}
