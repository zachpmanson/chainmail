// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { attach } from "../src/client/behaviour";

/** The renderer's scroll-spy builds one on mount, and jsdom has none. */
class NoopObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
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