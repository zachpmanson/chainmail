// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { withTransition } from "../src/lib/viewTransition";

/**
 * The animation a view switch is handed to, as a function of the document — no
 * React, no pane: what is asserted is what the browser is told to do, and that
 * the change happens either way.
 *
 * Both callers of this are covered here rather than through their own trees: the
 * page's timeline/columns switch and the pane's tree switch differ in how they
 * apply the change (DOM classes, React state) and not at all in what the
 * transition is — which messages are named, how many, and what a browser that
 * cannot do this is asked to do instead.
 */
const doc = () => {
  const bubbles = [0, 1, 2].map((i) => {
    const el = document.createElement("div");
    el.className = "msg";
    el.id = `entry-${i}`;
    document.body.append(el);
    return el;
  });
  return { bubbles, cleanup: () => bubbles.forEach((b) => b.remove()) };
};

describe("naming the bubbles a view switch moves", () => {
  it("names the messages on screen before the change, and clears the names after", async () => {
    const { bubbles, cleanup } = doc();
    const named: string[][] = [];
    const cleared: string[][] = [];
    Object.assign(document, {
      startViewTransition: (cb: () => void) => {
        named.push(bubbles.map((b) => b.style.viewTransitionName));
        cb();
        cleared.push(bubbles.map((b) => b.style.viewTransitionName));
        return { finished: Promise.resolve() };
      },
    });
    const applied: string[] = [];
    try {
      withTransition(document, () => applied.push("changed"));
      // the change happens inside the transition, not around it: the browser takes
      // its second snapshot when the callback returns
      expect(applied).toEqual(["changed"]);
      expect(named).toEqual([["entry-0", "entry-1", "entry-2"]]);
      // still named while the transition runs, so the browser can morph them
      expect(cleared).toEqual([["entry-0", "entry-1", "entry-2"]]);
      // and cleared once it is finished, so a later transition names its own — an
      // element left named would be excluded from the next one's root snapshot
      await Promise.resolve();
      expect(bubbles.map((b) => b.style.viewTransitionName)).toEqual(["", "", ""]);
    } finally {
      Object.assign(document, { startViewTransition: undefined });
      cleanup();
    }
  });

  it("names the bubbles the change made, not only the ones it named first", async () => {
    const { cleanup } = doc();
    const orphaned: HTMLElement[] = [];
    const live = () => [...document.querySelectorAll<HTMLElement>(".msg")];
    const namesOn = () => live().map((e) => e.style.viewTransitionName);
    const after: string[][] = [];
    Object.assign(document, {
      startViewTransition: (cb: () => void) => {
        cb();
        after.push(namesOn());
        return { finished: Promise.resolve() };
      },
    });
    try {
      withTransition(document, () => {
        // A change that re-parents a bubble makes a new node for it — how the tree
        // switch moves a reply into its container, which React cannot do by moving
        // the element it drew. The name taken before this is on a node that is no
        // longer in the document, and an unnamed pair does not morph: the browser
        // falls back to crossfading the whole root, which is the animation failing
        // quietly rather than not happening.
        for (const b of live()) {
          const fresh = b.cloneNode(true) as HTMLElement;
          fresh.style.viewTransitionName = "";
          orphaned.push(b);
          b.replaceWith(fresh);
        }
      });
      // so they are named twice: once on the way in, once on the nodes the change
      // left behind, which are the ones the browser's second snapshot is taken of
      expect(after).toEqual([["entry-0", "entry-1", "entry-2"]]);
      await Promise.resolve();
      // and both passes are cleared, including the nodes that left the document:
      // an element left named is excluded from the next transition's root snapshot
      expect(namesOn()).toEqual(["", "", ""]);
      expect(orphaned.map((b) => b.style.viewTransitionName)).toEqual(["", "", ""]);
    } finally {
      Object.assign(document, { startViewTransition: undefined });
      cleanup();
    }
  });

  it("changes the view without one where the browser has none", () => {
    const { cleanup } = doc();
    const applied: string[] = [];
    try {
      withTransition(document, () => applied.push("changed"));
      expect(applied).toEqual(["changed"]);
    } finally {
      cleanup();
    }
  });

  it("changes the view without one where the reader asked for no motion", () => {
    const { bubbles, cleanup } = doc();
    const realMatch = window.matchMedia;
    let asked = 0;
    Object.assign(window, {
      matchMedia: (q: string) => {
        asked++;
        return { matches: q.includes("prefers-reduced-motion"), media: q, addEventListener() {}, removeEventListener() {} };
      },
    });
    let calls = 0;
    Object.assign(document, { startViewTransition: () => { calls++; return { finished: Promise.resolve() }; } });
    try {
      const applied: string[] = [];
      withTransition(document, () => applied.push("changed"));
      expect(applied).toEqual(["changed"]);
      expect(asked).toBe(1);
      // asked, and then not used: the switch is a request to read the thread the
      // other way, not a request to watch a film
      expect(calls).toBe(0);
      expect(bubbles.map((b) => b.style.viewTransitionName)).toEqual(["", "", ""]);
    } finally {
      Object.assign(window, { matchMedia: realMatch });
      Object.assign(document, { startViewTransition: undefined });
      cleanup();
    }
  });

  it("names nothing that is off screen, and stops at two dozen", () => {
    const { cleanup } = doc();
    // jsdom lays nothing out, so an element's own rect is stubbed to be the way to
    // say "this one is a screen away" — off screen cannot be perceived, and
    // naming it would be work the browser does for nobody
    const far = document.createElement("div");
    far.className = "msg";
    far.id = "entry-far";
    far.getBoundingClientRect = () => ({ top: 99999, bottom: 100000 }) as DOMRect;
    document.body.append(far);
    const many = [...Array(30)].map((_, i) => {
      const el = document.createElement("div");
      el.className = "msg";
      el.id = `m${i}`;
      document.body.append(el);
      return el;
    });

    const names: string[][] = [];
    Object.assign(document, {
      startViewTransition: (cb: () => void) => {
        names.push([...document.querySelectorAll<HTMLElement>(".msg")].map((e) => e.style.viewTransitionName));
        cb();
        return { finished: Promise.resolve() };
      },
    });
    try {
      withTransition(document, () => {});
    } finally {
      Object.assign(document, { startViewTransition: undefined });
      far.remove();
      many.forEach((m) => m.remove());
      cleanup();
    }
    const named = names[0]!.filter(Boolean);
    expect(named).toHaveLength(24);
    expect(named).not.toContain("entry-far");
    // the ones it did name are the ones it met first, in document order
    expect(named[0]).toBe("entry-0");
  });
});


/**
 * The other half of a view switch: the text, re-flowed.
 *
 * A bubble that moves into the tree is indented, so it is a narrower box and its
 * lines break somewhere else. A named bubble is morphed as one image, and all the
 * browser can do with one image of a paragraph is stretch it onto the new box —
 * the words smear instead of moving to their new lines. So each word is wrapped in
 * a span of its own and named as well, and the browser has a pair of positions to
 * move each one between. The rules about which words get a name are the
 * reference's (box-toggle.html); the promise about a message is that nothing about
 * it changes except where its words sit.
 */
const bodied = (id: string, html: string) => {
  const el = document.createElement("div");
  el.className = "msg";
  el.id = id;
  const bd = document.createElement("div");
  bd.className = "bd";
  bd.innerHTML = html;
  el.append(bd);
  document.body.append(el);
  return { el, bd, cleanup: () => el.remove() };
};

/** The words a body was wrapped into, in the order the walk found them. */
const wordsOf = (root: Element | Document) =>
  [...root.querySelectorAll<HTMLElement>("[data-vtword]")];

/** What each of a bubble's words is currently named. Empty string is a word with
 *  no name, which is a word the browser will not move on its own. */
const namesOf = (el: HTMLElement) =>
  wordsOf(el.querySelector(".bd")!).map((w) => w.style.viewTransitionName);

/** The browser, stubbed: run `body` with the callback the browser would hand it,
 *  which is what lets a test look before the change, look after it, and step
 *  between the two passes. */
const stubbing = (body: (cb: () => void) => void) =>
  Object.assign(document, {
    startViewTransition: (cb: () => void) => {
      body(cb);
      return { finished: Promise.resolve() };
    },
  });

const unstub = () => Object.assign(document, { startViewTransition: undefined });

describe("naming the words a view switch re-flows", () => {
  it("wraps each word in a span of its own and names it, changing nothing else about the text", async () => {
    const { el, bd, cleanup } = bodied("entry-0", "<p>one two&nbsp; three</p>");
    const text = bd.textContent;
    let before: string[] = [];
    let after: string[] = [];
    stubbing((cb) => {
      before = namesOf(el);
      cb();
      after = namesOf(el);
    });
    try {
      withTransition(document, () => {});
      // one span per word, and the same characters between them: an element
      // boundary does not change how a run of whitespace is drawn, so the body
      // still says exactly what it said
      expect(wordsOf(bd)).toHaveLength(3);
      expect(bd.textContent).toBe(text);
      // named before the browser takes its first snapshot, and named again on the
      // nodes the change left behind — a React-drawn tree makes new bubbles with
      // fresh, unwrapped bodies, and the second snapshot is taken of those
      expect(before).toEqual(["entry-0-w-0", "entry-0-w-1", "entry-0-w-2"]);
      expect(after).toEqual(["entry-0-w-0", "entry-0-w-1", "entry-0-w-2"]);
      // and the bubble is still named, so the card and the words on it are one
      // change rather than two
      expect(el.style.viewTransitionName).toBe("entry-0");
      // cleared once the transition is over, so a later switch names its own — an
      // element left named is excluded from the next transition's root snapshot
      await Promise.resolve();
      expect(namesOf(el)).toEqual(["", "", ""]);
      expect(el.style.viewTransitionName).toBe("");
    } finally {
      unstub();
      cleanup();
    }
  });

  it("names a word after the bubble it is on, so a re-ordered and re-created body keeps its own names", () => {
    const a = bodied("entry-0", "<p>alpha beta</p>");
    const b = bodied("entry-1", "<p>gamma delta</p>");
    let after: string[] = [];
    const moved: HTMLElement[] = [];
    stubbing((cb) => {
      cb();
      after = wordsOf(document.body).map((w) => w.style.viewTransitionName);
    });
    try {
      withTransition(document, () => {
        // What drawing the tree does to the bubbles that move: they change places,
        // and React draws them as new nodes with new bodies, so the names taken
        // before the change are on elements that have left the document.
        const fresh = document.createElement("div");
        fresh.className = "msg";
        fresh.id = "entry-1";
        const body = document.createElement("div");
        body.className = "bd";
        body.innerHTML = "<p>gamma delta</p>";
        fresh.append(body);
        b.el.replaceWith(fresh);
        document.body.prepend(fresh);
        moved.push(fresh);
      });
      // A name taken from a place in the document would have paired alpha with
      // gamma, because the tree reorders the bubbles. Anchored to the bubble, each
      // word keeps the name its own message gave it.
      expect(after).toEqual(["entry-1-w-0", "entry-1-w-1", "entry-0-w-0", "entry-0-w-1"]);
    } finally {
      unstub();
      a.cleanup();
      b.cleanup();
      moved.forEach((el) => el.remove());
    }
  });

  it("names no word that is off the visible page, and takes the name back from one that leaves it", () => {
    const { el, bd, cleanup } = bodied("entry-0", "<p>one two</p>");
    // A first switch, only to wrap the body: the spans have to exist before one of
    // them can be said to be a screen away.
    stubbing((cb) => cb());
    withTransition(document, () => {});
    unstub();

    const two = wordsOf(bd)[1];
    const far = { top: 99999, bottom: 100000 } as DOMRect;
    const near = { top: 0, bottom: 0 } as DOMRect;
    let away = true;
    two!.getBoundingClientRect = () => (away ? far : near);

    let offscreen: string[] = [];
    stubbing((cb) => {
      offscreen = namesOf(el);
      cb();
    });
    try {
      withTransition(document, () => {});
      // A named word is drawn above the page, so one that is scrolled out of the
      // pane would be drawn outside the pane for the length of the switch — worse
      // than not animating it at all
      expect(offscreen).toEqual(["entry-0-w-0", ""]);
    } finally {
      unstub();
    }

    // The other rule, from the reference: a word the change takes out of the
    // visible page loses its name and fades out where it was, rather than flying
    // to a place the reader cannot see.
    away = false;
    let first: string[] = [];
    let second: string[] = [];
    stubbing((cb) => {
      first = namesOf(el);
      away = true;
      cb();
      second = namesOf(el);
    });
    try {
      withTransition(document, () => {});
      expect(first).toEqual(["entry-0-w-0", "entry-0-w-1"]);
      expect(second).toEqual(["entry-0-w-0", ""]);
    } finally {
      unstub();
      cleanup();
    }
  });

  it("wraps a body written verbatim not at all", () => {
    const { bd, cleanup } = bodied("entry-0", "<p>a word</p><pre>one  two\nthree</pre>");
    const text = bd.textContent;
    stubbing((cb) => cb());
    try {
      withTransition(document, () => {});
      // The paragraph's two words are wrapped. The block's own spacing is markup
      // drawn as it was written, and a span in the middle of it would be a change
      // to the rendering rather than to where a word sits.
      expect(wordsOf(bd).map((w) => w.textContent)).toEqual(["a", "word"]);
      expect(bd.querySelector("pre")!.textContent).toBe("one  two\nthree");
      expect(bd.textContent).toBe(text);
    } finally {
      unstub();
      cleanup();
    }
  });

  it("wraps a body once, however many times it is switched", async () => {
    const { el, bd, cleanup } = bodied("entry-0", "<p>one two three</p>");
    const text = bd.textContent;
    stubbing((cb) => cb());
    try {
      withTransition(document, () => {});
      withTransition(document, () => {});
      withTransition(document, () => {});
      // The wraps are left in place rather than taken out when the transition
      // ends: they are what makes the next switch cheap, and a span with no rule
      // on it is the word it contains, in the line it was always in.
      await Promise.resolve();
      expect(wordsOf(bd)).toHaveLength(3);
      expect(bd.textContent).toBe(text);
      expect(el.style.viewTransitionName).toBe("");
    } finally {
      unstub();
      cleanup();
    }
  });

  it("leaves the bodies whole where the reader asked for no motion", () => {
    const { bd, cleanup } = bodied("entry-0", "<p>one two</p>");
    const realMatch = window.matchMedia;
    Object.assign(window, {
      matchMedia: (q: string) => ({
        matches: q.includes("prefers-reduced-motion"),
        media: q,
        addEventListener() {},
        removeEventListener() {},
      }),
    });
    stubbing((cb) => cb());
    try {
      withTransition(document, () => {});
      // nothing is wrapped on that path: a reader who wants no motion is not a
      // reader who wants their message bodies taken apart for an animation that
      // will not be drawn
      expect(wordsOf(bd)).toHaveLength(0);
      expect(bd.querySelector("p")!.textContent).toBe("one two");
    } finally {
      Object.assign(window, { matchMedia: realMatch });
      unstub();
      cleanup();
    }
  });

  it("stops naming words after four hundred of them, and wraps the rest", () => {
    const many = Array.from({ length: 450 }, (_, i) => `w${i}`).join(" ");
    const { bd, cleanup } = bodied("entry-0", `<p>${many}</p>`);
    stubbing((cb) => cb());
    try {
      withTransition(document, () => {});
      const spans = wordsOf(bd);
      // all of them wrapped, so the next switch finds words rather than text and
      // pays nothing to read them again
      expect(spans).toHaveLength(450);
      // but only the first four hundred named: each name is a snapshot and an
      // animation of its own, and past that the switch stops being a switch
      expect(spans.filter((w) => w.style.viewTransitionName !== "")).toHaveLength(400);
      expect(spans[399]!.style.viewTransitionName).toBe("entry-0-w-399");
      expect(spans[400]!.style.viewTransitionName).toBe("");
    } finally {
      unstub();
      cleanup();
    }
  });
});
