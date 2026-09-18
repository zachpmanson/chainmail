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
