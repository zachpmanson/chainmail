/**
 * A change to how a thread is DRAWN, animated: the same bubbles, moved.
 *
 * Two views of one thread differ by where each bubble sits — the transcript's
 * order and the reply tree's — and a redraw that swaps one for the other without
 * a transition is a page that has been replaced rather than a thread that has
 * been rearranged: every bubble the reader was looking at is in a new place, at
 * once, with nothing saying which one went where. Naming the bubbles and letting
 * the browser morph them between positions is the whole of the effect, and the
 * names are the bubble's own DOM id — the entry's anchor, which is the entry's
 * place in the thread the corpus sent rather than its place on the page (see
 * ThreadMessages), so it means the same message in both views.
 *
 * Shared by the two callers that rearrange a transcript: the page's own
 * timeline/columns switch, which is DOM-attached behaviour (`attach`), and the
 * reading pane's tree switch, which is React state (ThreadPane). What the React
 * caller passes as `apply` has to land in the DOM before the browser takes its
 * second snapshot, so it wraps its state change in `flushSync` — see ThreadPane.
 *
 * The naming is done TWICE — before the change and again inside it — because the
 * change can replace the elements it named. Drawing the replies as a tree puts
 * every bubble inside a container of its own (see ThreadMessages), which is a new
 * parent, so React makes new DOM nodes for them rather than moving the old ones;
 * a name set on the old node is a name the browser cannot find on the other side
 * of the change, and an unnamed pair is not morphable: the whole switch degrades
 * to the root crossfade, which is exactly what a reader sees as "it just faded".
 * Naming again after `apply` names the bubbles the browser is about to snapshot,
 * and a name is all the pairing needs — the node objects are allowed to differ.
 *
 * Not a React hook, and not a component: what it needs is the document, and the
 * two callers hold it differently. Both go through this one function because the
 * behaviour it encodes has to be the same in both places — which messages are
 * named, how many, and what happens when the browser cannot do it.
 */

/**
 * Run `apply` as a view transition, naming only the entries on screen before it:
 * dozens of transition groups is needless work, and the ones off screen cannot
 * be perceived. Names are cleared afterwards so they never affect a later
 * transition.
 *
 * A browser without `startViewTransition`, and a reader who has asked for
 * reduced motion, get the change itself and no animation — the switch is a
 * request to see a thread the other way, not a request to watch a film.
 */
export function withTransition(doc: Document, apply: () => void) {
  type WithVT = Document & { startViewTransition?: (cb: () => void) => { finished: Promise<void> } };
  const d = doc as WithVT;
  const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  if (!d.startViewTransition || reduce) { apply(); return; }

  // Everything named, across both passes, so clearing afterwards leaves nothing
  // named whether or not the change kept the node — an element left named is
  // excluded from the next transition's root snapshot.
  const named = new Set<HTMLElement>();
  const nameOnScreen = () => {
    const vh = window.innerHeight;
    let count = 0;
    for (const el of doc.querySelectorAll<HTMLElement>(".msg[id], .sys[id]")) {
      if (count >= 24) break;
      const r = el.getBoundingClientRect();
      if (r.bottom <= -vh * 0.25 || r.top >= vh * 1.25) continue;
      el.style.viewTransitionName = el.id;
      named.add(el);
      count++;
    }
  };
  const clear = () => {
    for (const el of named) el.style.viewTransitionName = "";
    named.clear();
  };

  nameOnScreen();
  d.startViewTransition(() => {
    apply();
    // The bubbles are the change's own output, and the ones `apply` left behind
    // are the ones the browser names its second snapshot from.
    nameOnScreen();
  }).finished.then(clear, clear);
}
