/**
 * A change to how a thread is DRAWN, animated: the same bubbles, moved — and, on
 * them, the same words, re-flowed.
 *
 * Two views of one thread differ by where each bubble sits — the transcript's
 * order and the reply tree's — and a redraw that swaps one for the other without
 * a transition is a page that has been replaced rather than a thread that has
 * been rearranged: every bubble the reader was looking at is in a new place, at
 * once, with nothing saying which one went where. Naming the bubbles and letting
 * the browser morph them between positions is the whole of what moves the cards,
 * and the names are the bubble's own DOM id — the entry's anchor, which is the
 * entry's place in the thread the corpus sent rather than its place on the page
 * (see ThreadMessages), so it means the same message in both views.
 *
 * A bubble that moves does not only move. The tree indents every reply by how
 * deep it is (see .ibread .stream .replies), so a bubble lower in the graph is a
 * narrower box than the same bubble in the transcript's flat order — and every
 * line of its body breaks somewhere else. A named bubble is morphed as one
 * picture, which is the right animation for the card and the wrong one for the
 * words on it: all the browser can do with one image of a paragraph is stretch it
 * onto the box the paragraph now lives in, so the text smears rather than
 * re-flowing, and the reader watching the switch sees the words warp instead of
 * arriving on their new lines. So the words are named too, one span each: two
 * spans with the same name are the same word on both sides of the change, and the
 * browser puts each one where it now sits. This is the effect the reference for
 * this behaviour is built on (box-toggle.html) and the whole of what it does —
 * names on the bubbles, names on the words, and defaults for the rest.
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
 * A replaced node has a replaced body as well, so the second pass reads the words
 * again for the same reason it reads the bubbles again: it has to name the words
 * the second snapshot will hold, and they are in the nodes `apply` left behind.
 *
 * Not a React hook, and not a component: what it needs is the document, and the
 * two callers hold it differently. Both go through this one function because the
 * behaviour it encodes has to be the same in both places — which messages are
 * named, which words, how many, and what happens when the browser cannot do it.
 */

/**
 * The attribute that marks a span this module made.
 *
 * It is what keeps the second pass, and every later switch, from wrapping a word
 * inside the span it is already in: the walk takes a marked element as one word
 * rather than descending through it. So a body is wrapped once and read cheaply
 * for the rest of the session, and re-reading a body the change re-created is the
 * only time the wrapping is done again.
 *
 * The wraps are left in place rather than taken out when the transition ends —
 * the reference leaves its own in, and they are what makes the next switch cheap.
 * They change no text and draw nothing: a span with no rule on it is the word it
 * contains, in the line it was always in.
 */
const WORD = "data-vtword";

/** Markup whose text is drawn as it was written rather than as a run of words the
 *  browser may re-break: a code block's own spacing, a drawing's coordinates, a
 *  textarea's value. A span in the middle of one of those is a change to the
 *  rendering, and this module's one promise about a message is that it changes
 *  where a word sits and nothing else about it — so the text inside these is left
 *  whole and moves with its bubble, which is what the whole body did before any of
 *  this existed. `white-space` is checked as well as the tag, because the property
 *  is what actually decides (see collectWords). */
const VERBATIM = new Set([
  "PRE",
  "CODE",
  "TEXTAREA",
  "SCRIPT",
  "STYLE",
  "SVG",
  "MATH",
  "TITLE",
  "NOSCRIPT",
]);

/** How many words one switch may name.
 *
 *  Every name is a transition group of its own — an element snapshot, a pair of
 *  images and an animation — and a thread of thirty messages holds thousands of
 *  words. Past this the words the reader is looking at are the ones that re-flow
 *  and the rest travel with their bubbles, which is what the switch did before any
 *  of this existed; the alternative is a button press that spends a second
 *  building groups for words below the fold. The cap is generous enough to cover
 *  several screens of a thread and small enough to leave the switch a switch. */
const WORD_CAP = 400;

/** Attachment chips are larger transition groups than words, but a thread can
 *  still have many of them. Bound the work just as the word pass is bounded. */
const ATTACHMENT_CAP = 100;

/**
 * Every word under `root`, in document order, wrapping the ones that are not
 * already wrapped. What was already a word is taken as one rather than descended
 * through, which is how a second pass reads the same body without wrapping it a
 * second time.
 *
 * A text node is split at its whitespace and the whitespace is kept verbatim
 * between the spans, so the body's own text is exactly what it was: what the
 * browser is handed is the same words separated by the same characters, and how
 * those collapsible runs are drawn does not depend on where the element boundaries
 * fall (normal white-space collapsing carries across inline boxes). A run of
 * whitespace with no words in it is left alone entirely — there is nothing there
 * to move.
 *
 * `white-space: pre` and its cousins keep their own line breaks and spacing, so a
 * subtree drawn that way is skipped: what a word of it would do on the other side
 * of the change is nothing, and re-breaking is not this module's to do. The
 * computed value is what is asked, because that is the property that decides —
 * the tag list above it is only a fast path, and a rule can give `pre` to anything.
 */
function collectWords(doc: Document, root: HTMLElement, out: HTMLElement[]) {
  for (const child of Array.from(root.childNodes)) {
    if (child.nodeType === Node.TEXT_NODE) {
      const text = child as Text;
      if (!/\S/.test(text.data)) continue;
      const frag = doc.createDocumentFragment();
      for (const piece of text.data.split(/(\s+)/)) {
        if (piece === "") continue;
        if (/^\s+$/.test(piece)) {
          frag.append(doc.createTextNode(piece));
          continue;
        }
        const span = doc.createElement("span");
        span.setAttribute(WORD, "");
        span.textContent = piece;
        frag.append(span);
        out.push(span);
      }
      text.replaceWith(frag);
      continue;
    }
    if (child.nodeType !== Node.ELEMENT_NODE) continue;
    const el = child as HTMLElement;
    if (el.hasAttribute(WORD)) {
      out.push(el);
      continue;
    }
    if (VERBATIM.has(el.tagName)) continue;
    if (doc.defaultView?.getComputedStyle(el).whiteSpace.startsWith("pre")) continue;
    collectWords(doc, el, out);
  }
}

/**
 * Run `apply` as a view transition, naming only the entries on screen before it:
 * dozens of transition groups is needless work, and the ones off screen cannot
 * be perceived. Names are cleared afterwards so they never affect a later
 * transition.
 *
 * A browser without `startViewTransition`, and a reader who has asked for
 * reduced motion, get the change itself and no animation — the switch is a
 * request to see a thread the other way, not a request to watch a film. Nothing
 * is wrapped on that path either: a reader who wants no motion is not a reader
 * who wants their message bodies taken apart and rebuilt for one that will not be
 * drawn.
 */
export function withTransition(doc: Document, apply: () => void) {
  type WithVT = Document & {
    startViewTransition?: (cb: () => void) => { finished: Promise<void> };
  };
  const d = doc as WithVT;
  const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  if (!d.startViewTransition || reduce) {
    apply();
    return;
  }

  // Everything named, across both passes, so clearing afterwards leaves nothing
  // named whether or not the change kept the node — an element left named is
  // excluded from the next transition's root snapshot.
  const named = new Set<HTMLElement>();

  /** Whether an element is close enough to the window to be watched moving. The
   *  quarter-screen of slack is the page's own margin rather than the pane's: a
   *  bubble the reader is about to scroll to is worth naming, and one a screen
   *  away is not. */
  const onScreen = (el: HTMLElement) => {
    const vh = window.innerHeight;
    const r = el.getBoundingClientRect();
    return r.bottom > -vh * 0.25 && r.top < vh * 1.25;
  };

  const nameBubbles = () => {
    let count = 0;
    for (const el of doc.querySelectorAll<HTMLElement>(".msg[id], .sys[id]")) {
      if (count >= 24) break;
      if (!onScreen(el)) continue;
      el.style.viewTransitionName = el.id;
      named.add(el);
      count++;
    }
  };

  // Which words the first pass named, by the name it gave them.
  //
  // A word's name is its bubble's id and the word's own place in that bubble's
  // body, and not a place in the document: the change reorders and re-parents the
  // bubbles, so a document-order name would mean a different word on the far side
  // and the browser would fly one message's words into another's. Anchored to the
  // bubble, the name survives the move — the bubble's id is the entry's place in
  // the thread the corpus sent (see ThreadMessages), which is the same entry in
  // both views.
  //
  // The second pass names a word only where the first pass did, so a word the
  // change brought into view arrives with the bubble it is on rather than flying
  // in from nowhere, and a word the change took out of view loses its name and
  // fades out where it was — which is the reference's own rule and the right
  // reading of a word leaving the page: it is going somewhere the reader is not
  // looking.
  const chosen = new Set<string>();
  const nameWords = (first: boolean) => {
    let count = 0;
    for (const bubble of doc.querySelectorAll<HTMLElement>(".msg[id]")) {
      if (!onScreen(bubble)) continue;
      const body = bubble.querySelector<HTMLElement>(".bd");
      if (!body) continue;
      const words: HTMLElement[] = [];
      collectWords(doc, body, words);
      words.forEach((el, i) => {
        const key = `${bubble.id}-w-${i}`;
        const keep = count < WORD_CAP && onScreen(el) && (first || chosen.has(key));
        el.style.viewTransitionName = keep ? key : "";
        if (!keep) return;
        chosen.add(key);
        named.add(el);
        count++;
      });
    }
  };

  // Attachments are the chips on a message, not the thumbnail inside one: that
  // whole chip is what moves between line wraps and tree indentation. As with
  // words, pair by the message and its place in that message, and only name chips
  // visible on the first side so a newly-visible one does not fly in from outside
  // the reader's view.
  const chosenAttachments = new Set<string>();
  const nameAttachments = (first: boolean) => {
    let count = 0;
    for (const bubble of doc.querySelectorAll<HTMLElement>(".msg[id]")) {
      if (!onScreen(bubble)) continue;
      bubble.querySelectorAll<HTMLElement>(".att").forEach((el, i) => {
        const key = `${bubble.id}-a-${i}`;
        const keep =
          count < ATTACHMENT_CAP && onScreen(el) && (first || chosenAttachments.has(key));
        el.style.viewTransitionName = keep ? key : "";
        if (!keep) return;
        chosenAttachments.add(key);
        named.add(el);
        count++;
      });
    }
  };

  const clear = () => {
    for (const el of named) el.style.viewTransitionName = "";
    named.clear();
  };

  nameBubbles();
  nameWords(true);
  nameAttachments(true);
  d.startViewTransition(() => {
    apply();
    // The bubbles are the change's own output, and the ones `apply` left behind
    // are the ones the browser names its second snapshot from. The words are read
    // again for the same reason: a bubble React re-created has a name of its own
    // again but a body of fresh, unwrapped text, and the second snapshot is taken
    // of that text.
    nameBubbles();
    nameWords(false);
    nameAttachments(false);
  }).finished.then(clear, clear);
}
