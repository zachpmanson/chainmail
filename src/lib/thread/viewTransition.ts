// Words are named as well as bubbles, else the browser stretches each body as one image instead of re-flowing it.
// React callers must flushSync inside `apply` so the DOM updates before the second snapshot.

// Marks spans already wrapped, so later passes don't re-wrap them. Left in place after the transition.
const WORD = "data-vtword";

// Whitespace-sensitive content is never split into word spans; collectWords also checks computed `white-space`.
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

// Each name is its own transition group; past this cap, words just move with their bubble.
const WORD_CAP = 400;

const ATTACHMENT_CAP = 100;

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

  // An element left named is excluded from the next transition's root snapshot, so clear them all.
  const named = new Set<HTMLElement>();

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

  // Keyed by bubble id + index, not document order, so names survive reordering.
  // The second pass names only words the first did, so new arrivals don't fly in from nowhere.
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

  const chosenAttachments = new Set<string>();
  const nameAttachments = (first: boolean) => {
    let count = 0;
    for (const bubble of doc.querySelectorAll<HTMLElement>(".msg[id]")) {
      if (!onScreen(bubble)) continue;
      bubble.querySelectorAll<HTMLElement>("[data-attachment]").forEach((el, i) => {
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
    // `apply` can make React re-create the nodes, so name them again before the second snapshot.
    nameBubbles();
    nameWords(false);
    nameAttachments(false);
  }).finished.then(clear, clear);
}
