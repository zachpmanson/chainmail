/**
 * Reddit-style threading: replies drawn under the message they answer, indented
 * by how deep in the reply graph they are.
 *
 * A transcript is chronological, which is right for reading a conversation that
 * happened in time and wrong for reading one that forked: in a trail where three
 * people each answered the same question and one of them was answered again, the
 * flat order puts the answer to the answer under the next person's answer to
 * something else, and the reader has to hold the graph in their head to see who
 * was talking to whom. Nesting is the same graph drawn instead of remembered —
 * the reply arrow each bubble already carries (`ReplyLink`) is a link to a
 * message *above* rather than to one further up, and the indentation is how far
 * up it is.
 *
 * The order within a level is the transcript's own, and the clock still runs
 * down the page wherever the graph allows it: an answer is drawn after the
 * message it answers because it *was* sent after it, and only the siblings the
 * tree forces apart move. Nothing else about a bubble changes — the same
 * `Message`, the same reply link, the same anchoring — so the two views differ
 * by one number per message.
 *
 * Kept in the browser, like the list's width (see lib/panelWidth) and for the
 * same reason: which way a reader likes a thread drawn is about the reader and
 * not about the mailbox, so it is nothing the server should be told. It is a
 * fact about the window rather than about the account, which is also why it is
 * not a setting — and a browser that refuses to store it still gets the switch,
 * it just forgets between visits.
 */

/** One bubble in the drawn order, and how far in it is. */
export interface Nested<E> {
  entry: E;
  /** how many replies deep this message is: 0 is an opener, 1 answers an opener,
   *  and so on. The stylesheet turns it into an indent (see .ibread .stream .msg). */
  depth: number;
}

/** The entries in reply-tree order: each message followed by the replies to it,
 *  each of those followed by the replies to it, and so on.
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
 *    any it could not reach are drawn flat, in the transcript's order, at the end.
 *    A message that cannot be placed still has to be readable: dropping it would
 *    be the one failure a reader would not forgive this switch for.
 */
export function nest<E>(
  entries: E[],
  key: (e: E) => string,
  parent: (e: E) => string | undefined,
): Nested<E>[] {
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

  const out: Nested<E>[] = [];
  const drawn = new Set<string>();
  const walk = (e: E, depth: number) => {
    const k = key(e);
    if (drawn.has(k)) return;
    drawn.add(k);
    out.push({ entry: e, depth });
    for (const reply of children.get(k) ?? []) walk(reply, depth + 1);
  };
  for (const root of roots) walk(root, 0);
  // Anything left is in a cycle: reachable from no opener, and so unreachable
  // from here except by starting somewhere inside it.
  for (const e of entries) if (!drawn.has(key(e))) walk(e, 0);

  return out;
}

/** The switch's own key. Distinct from the page's `cm-tree`, which is the reply
 *  tree's *minimap*: the two are different questions about the same graph, and a
 *  reader who wants one does not necessarily want the other. */
const KEY = "cm-nest";

/** Whether the reader wants replies nested. Off to begin with: the transcript's
 *  own order is what a thread has always been drawn in here, and a switch that
 *  defaults to on would be this build deciding a reader's preference for them. */
export function readThreaded(): boolean {
  try {
    return localStorage.getItem(KEY) === "1";
  } catch {
    return false;
  }
}

/** Remember the switch, or forget it — "0" is as good as absent, and is written
 *  rather than removed so that a reader who turned it off is not a reader who
 *  has never chosen. */
export function rememberThreaded(on: boolean): void {
  try {
    localStorage.setItem(KEY, on ? "1" : "0");
  } catch {
    /* private mode: the switch still works, the choice just does not last */
  }
}
