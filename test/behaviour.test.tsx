// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { attach } from "../src/client/behaviour";

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