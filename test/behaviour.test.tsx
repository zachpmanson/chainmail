// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { attach, lineAt, messageAt } from "../src/client/behaviour";

/** The renderer's scroll-spy builds one on mount, and jsdom has none. The last
 *  one built is kept so a test can drive it: only the callback can say which
 *  entry is being read, and the panel's follow behaviour hangs off that. */
class NoopObserver {
  static last: NoopObserver | null = null;
  cb: (records: unknown[], obs: NoopObserver) => void;
  constructor(cb: (records: unknown[], obs: NoopObserver) => void) {
    this.cb = cb;
    NoopObserver.last = this;
  }
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
  /** The browser telling the spy about an entry, as it scrolls into the band. */
  enter(id: string, top = 0) {
    this.cb([{ target: document.getElementById(id)!, isIntersecting: true, boundingClientRect: { top } }], this);
  }
  leave(id: string) {
    this.cb([{ target: document.getElementById(id)!, isIntersecting: false, boundingClientRect: { top: 0 } }], this);
  }
}
(globalThis as { IntersectionObserver?: unknown }).IntersectionObserver = NoopObserver;

/** The pieces of the page the tree toggle touches: the toolbar button and the
 *  panel's two orientation rows (each with its own scroller). */
const page = () => `
  <button class="tbtn" id="maptog" type="button" aria-pressed="true"
          aria-label="Reply tree panel">tree</button>
  <button class="tbtn" id="plaintog" type="button">plain</button>
  <button class="tbtn" id="viewtog" type="button">columns</button>
  <aside class="mini" id="mini">
    <div class="vrow"><div class="mbody"></div></div>
    <div class="hrow"><div class="mbody"></div></div>
  </aside>
`;

const mount = () => {
  document.body.innerHTML = page();
  const detach = attach(document);
  const btn = document.getElementById("maptog") as HTMLButtonElement;
  const click = () =>
    btn.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
  return { detach, btn, click, body: document.body };
};

describe("the tree button's three states", () => {
  beforeEach(() => {
    localStorage.clear();
  });
  afterEach(() => {
    document.body.innerHTML = "";
    localStorage.clear();
  });

  it("defaults to vertical: no off class, no horizontal class", () => {
    const { detach, btn, body } = mount();
    expect(body.classList.contains("mapoff")).toBe(false);
    expect(body.classList.contains("tree-h")).toBe(false);
    expect(btn.getAttribute("aria-pressed")).toBe("true");
    detach();
  });

  it("cycles vertical → horizontal → off → vertical", () => {
    const { detach, click, body } = mount();

    click(); // → horizontal
    expect(body.classList.contains("tree-h")).toBe(true);
    expect(body.classList.contains("mapoff")).toBe(false);
    expect(localStorage.getItem("cm-tree")).toBe("h");

    click(); // → off
    expect(body.classList.contains("mapoff")).toBe(true);
    expect(body.classList.contains("tree-h")).toBe(false);
    expect(localStorage.getItem("cm-tree")).toBe("off");

    click(); // → vertical again
    expect(body.classList.contains("mapoff")).toBe(false);
    expect(body.classList.contains("tree-h")).toBe(false);
    expect(localStorage.getItem("cm-tree")).toBe("v");

    detach();
  });

  it("clears the reserved column in horizontal mode and restores it in vertical", () => {
    const { detach, click, body } = mount();
    // vertical reserves a panel-width column (0 in jsdom, but the property is set)
    expect(body.style.getPropertyValue("--panel")).toBe("0px");

    click(); // → horizontal: the strip spans the viewport, no column
    expect(body.style.getPropertyValue("--panel")).toBe("");

    click(); // → off: nothing reserved either
    expect(body.style.getPropertyValue("--panel")).toBe("");

    click(); // → vertical again
    expect(body.style.getPropertyValue("--panel")).toBe("0px");
    detach();
  });

  it("names the current state on the button", () => {
    const { detach, click, btn } = mount();
    expect(btn.getAttribute("aria-label")).toContain("vertical");
    click();
    expect(btn.getAttribute("aria-label")).toContain("horizontal");
    click();
    expect(btn.getAttribute("aria-pressed")).toBe("false");
    detach();
  });

  it("migrates the old boolean key: '1' (hidden) becomes off, '0' stays vertical", () => {
    localStorage.setItem("cm-tree", "1");
    const off = mount();
    expect(off.body.classList.contains("mapoff")).toBe(true);
    off.detach();

    document.body.innerHTML = "";
    localStorage.setItem("cm-tree", "0");
    const on = mount();
    expect(on.body.classList.contains("mapoff")).toBe(false);
    expect(on.body.classList.contains("tree-h")).toBe(false);
    on.detach();
  });

  it("remembers a horizontal choice across reloads", () => {
    localStorage.setItem("cm-tree", "h");
    const { detach, body } = mount();
    expect(body.classList.contains("tree-h")).toBe(true);
    expect(body.classList.contains("mapoff")).toBe(false);
    detach();
  });
});

/* The panel follows the entry being read, but must not undo a scroll the reader
   made in the panel itself. jsdom has no layout, so the two rectangles the
   follow logic compares — the entry's and the scroller's — are stubbed. */
describe("the tree panel's scroll follow", () => {
  const PAGE = `
    <div class="msg" id="a"></div>
    <div class="msg" id="b"></div>
    <aside class="mini" id="mini">
      <div class="vrow"><div class="mbody"><svg>
        <g class="nd o1 rt" data-id="a" data-p=""></g>
        <g class="nd o1" data-id="b" data-p="a"></g>
      </svg></div></div>
      <div class="hrow"><div class="mbody"></div></div>
    </aside>
  `;

  const rect = (top: number, height = 10) =>
    ({ top, bottom: top + height, left: 0, right: 0, width: 0, height, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect;
  const place = (el: Element, top: number) => {
    (el as HTMLElement).getBoundingClientRect = () => rect(top);
  };

  const mountGraph = () => {
    document.body.innerHTML = PAGE;
    const detach = attach(document);
    const scroller = document.querySelector<HTMLElement>(".vrow .mbody")!;
    place(scroller, 0);
    Object.defineProperty(scroller, "clientHeight", { value: 100, configurable: true });
    // jsdom clamps scrollTop against a scrollHeight it does not compute, so it
    // is replaced with a plain number this test can read
    let top = 0;
    Object.defineProperty(scroller, "scrollTop", {
      get: () => top,
      set: (v: number) => { top = v; },
      configurable: true,
    });
    place(document.querySelector(".nd[data-id=a]")!, 400);
    place(document.querySelector(".nd[data-id=b]")!, 1200);
    return { detach, scroller };
  };

  /** The reader scrolling the panel themselves: a wheel, then the offset. */
  const readerScrolls = (scroller: HTMLElement, to: number) => {
    scroller.dispatchEvent(new Event("wheel"));
    scroller.scrollTop = to;
    scroller.dispatchEvent(new Event("scroll"));
  };

  const hover = (id: string) => {
    const el = document.getElementById(id)!;
    el.dispatchEvent(new Event("mouseenter"));
    el.dispatchEvent(new Event("mouseleave"));
  };

  beforeEach(() => {
    // the earlier suite leaves its orientation class on the body, and the panel
    // follows the scroller for whichever orientation that is
    document.body.className = "";
  });

  afterEach(() => {
    document.body.innerHTML = "";
    localStorage.clear();
    vi.useRealTimers();
  });

  it("centres the panel on the entry being read", () => {
    const { detach, scroller } = mountGraph();
    NoopObserver.last!.enter("a");
    // 400 - 0 - clientHeight/2
    expect(scroller.scrollTop).toBe(350);
    detach();
  });

  it("leaves the panel alone when the reader has scrolled it", () => {
    const { detach, scroller } = mountGraph();
    NoopObserver.last!.enter("a");
    readerScrolls(scroller, 20);
    // the same entry lighting again — a hover, a spy re-report — must not yank
    // the panel back to where it was centred
    hover("a");
    expect(scroller.scrollTop).toBe(20);
    detach();
  });

  it("does not chase a new entry while the reader is in the panel", () => {
    const { detach, scroller } = mountGraph();
    NoopObserver.last!.enter("a");
    readerScrolls(scroller, 20);
    NoopObserver.last!.leave("a");
    NoopObserver.last!.enter("b");
    expect(scroller.scrollTop).toBe(20);
    detach();
  });

  it("follows again once the reader has stopped", () => {
    const { detach, scroller } = mountGraph();
    vi.useFakeTimers();
    NoopObserver.last!.enter("a");
    readerScrolls(scroller, 20);
    vi.advanceTimersByTime(3000);
    NoopObserver.last!.leave("a");
    NoopObserver.last!.enter("b");
    // 20 + (1200 - 0 - clientHeight/2)
    expect(scroller.scrollTop).toBe(1170);
    detach();
  });
});
/**
 * The reading pane's reply lines, as the reader's pointer meets them.
 *
 * jsdom lays nothing out, so every box's rect is stubbed: what is being asserted is
 * the arithmetic between a point and a line — which line a point is on, and which
 * message that line's mark lands on — rather than anything jsdom could compute.
 */
const linePane = () => {
  document.body.innerHTML = `
    <div class="ibread"><div class="stream">
      <div class="msg" id="entry-0"><div class="bub">root</div></div>
      <div class="replies">
        <div class="msg" id="entry-1"><div class="bub">answer</div></div>
        <div class="replies">
          <div class="msg" id="entry-2"><div class="bub">answer to the answer</div></div>
        </div>
      </div>
      <div class="msg" id="entry-9"><div class="bub">a separate message, no answers</div></div>
    </div></div>`;
  //      x:  100        200        300
  //     ┌──────────────────────────────┐  entry-0      y 0..40
  //     │ ┌────────────────────────────┐│  line of entry-0 at 116, entry-1 at 216
  const rects: Record<string, [number, number, number, number]> = {
    "entry-0": [100, 0, 400, 40],
    "entry-1": [130, 50, 400, 90],
    "entry-2": [160, 100, 400, 140],
    "entry-9": [100, 150, 400, 190],
  };
  document.querySelectorAll<HTMLElement>(".msg").forEach((el) => {
    const [left, top, right, bottom] = rects[el.id]!;
    el.getBoundingClientRect = () => ({ left, top, right, bottom, width: right - left, height: bottom - top }) as DOMRect;
  });
  document.querySelectorAll<HTMLElement>(".replies").forEach((el, i) => {
    // the outer line hangs under entry-0 (at 116) and the inner one under entry-1 (at 216)
    const left = 116 + i * 100;
    el.getBoundingClientRect = () => ({ left, top: 50 + i * 50, right: 400, bottom: 140 + i * -0 }) as DOMRect;
  });
  return { detach: attach(document) };
};

const pointAt = (x: number, y: number, target?: Element) => {
  const ev = new MouseEvent("mousemove", { clientX: x, clientY: y, bubbles: true });
  (target ?? document.querySelector(".ibread .stream")!).dispatchEvent(ev);
};

describe("pointing at a reply line", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("takes the line under the pointer, and only within reach of it", () => {
    const { detach } = linePane();
    // On the rule itself, a few pixels either side of it, and not at arm's length:
    // a line is two pixels wide and a pointer is not.
    expect(lineAt(document, 116, 80)?.previousElementSibling?.id).toBe("entry-0");
    expect(lineAt(document, 111, 80)?.previousElementSibling?.id).toBe("entry-0");
    expect(lineAt(document, 121, 80)?.previousElementSibling?.id).toBe("entry-0");
    expect(lineAt(document, 140, 80)).toBeNull();
    // Above the line's first pixel and below its last, there is no line to be on.
    expect(lineAt(document, 116, 49)).toBeNull();
    expect(lineAt(document, 116, 80)).not.toBeNull();
    detach();
  });

  it("takes the nearest line where two of them run side by side", () => {
    const { detach } = linePane();
    // Both lines are in reach at their own x, and the inner one is the answer for a
    // point inside it: the lines nest a step apart, so the one furthest in is also
    // the one nearest the reader's pointer — and marking the outer one too would mark
    // a whole ancestry of messages from a single pointer.
    expect(lineAt(document, 216, 120)?.previousElementSibling?.id).toBe("entry-1");
    expect(lineAt(document, 116, 120)?.previousElementSibling?.id).toBe("entry-0");
    detach();
  });

  it("reads the space between two messages as the upper one's", () => {
    const { detach } = linePane();
    // The gap under a card is that card's own margin, and a pointer in it is still on
    // the card: without this the path would go out for eight pixels at every boundary
    // as a reader runs the pointer up a thread, since a gap between two bubbles has
    // neither a line nor a card in it to hold on to.
    expect(messageAt(document, 200, 92)?.id).toBe("entry-1");
    expect(messageAt(document, 200, 95)?.id).toBe("entry-1");
    // past the last card's own margin is nobody's
    expect(messageAt(document, 120, 202)).toBeNull();
    // and a point beside a card, in the gutter the indent opened, is not on it
    expect(messageAt(document, 110, 70)).toBeNull();
    detach();
  });

  it("lights every line a message descends through, and no bubbles", () => {
    const { detach } = linePane();
    const lines = () => document.querySelectorAll(".rhov").length;
    // entry-2 is the innermost answer: pointing at it lights the two lines it
    // descends through — the line under entry-1's answers, and the line under
    // entry-0's — which is the tree view's one answer to "where did this come from".
    pointAt(200, 120, document.getElementById("entry-2")!);
    expect(lines()).toBe(2);
    expect(document.querySelectorAll(".rhov.msg").length).toBe(0);

    // The line that hangs off entry-1 is a way of pointing at entry-1 — the inner
    // line belongs to entry-1's answers — so the very same line is lit, which is the
    // point of walking the DOM for both: pointing at a line and pointing at its
    // message cannot disagree about what the reader is asking about.
    pointAt(200, 70, document.getElementById("entry-1")!);
    const byBubble = [...document.querySelectorAll(".rhov")];
    expect(byBubble.length).toBe(1);
    pointAt(216, 120);
    expect([...document.querySelectorAll(".rhov")]).toEqual(byBubble);

    // A message shallower in the tree lights a shorter path: the lines above it, not
    // the ones below it — the line of a message's own answers says where *they* came
    // from, and the reader is not pointing at any of them.
    pointAt(200, 20, document.getElementById("entry-0")!);
    expect(lines()).toBe(0);

    // A message at the top of the tree has no lines above it at all
    pointAt(200, 170, document.getElementById("entry-9")!);
    expect(lines()).toBe(0);

    // Away from every line and every card, nothing is lit
    pointAt(200, 220);
    expect(lines()).toBe(0);
    detach();
  });

  it("keeps the path lit while the pointer stays on it, and drops it on the way out", () => {
    const { detach } = linePane();
    pointAt(216, 120);
    const lit = () => [...document.querySelectorAll(".rhov")].map((e) => e.tagName + (e.id || "line")).join(",");
    const once = lit();
    // The same path, reported again by the next few pixels of the same movement: not
    // one class is touched, because touching one would start its fade over — a mark
    // that is already lit must not flicker back in on every mouse event.
    const add = vi.spyOn(DOMTokenList.prototype, "add");
    pointAt(216, 124);
    pointAt(216, 128);
    expect(add).not.toHaveBeenCalled();
    add.mockRestore();
    expect(lit()).toBe(once);
    // and the pointer leaving the transcript ends the path where it ends
    document.querySelector(".ibread .stream")!.dispatchEvent(new MouseEvent("mouseleave", { bubbles: true }));
    expect(document.querySelectorAll(".rhov").length).toBe(0);
    detach();
  });
});

