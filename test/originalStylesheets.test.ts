// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { dropSchemeVariants, mountOriginal } from "../src/lib/message/original";

function group(children: object[], conditionText?: string) {
  const rules = [...children];
  const node = {
    conditionText,
    cssRules: rules as unknown as CSSRuleList,
    deleteRule(index: number) {
      rules.splice(index, 1);
    },
  };
  return { node, rules };
}

function opaque(error: unknown = new DOMException("Cross-origin stylesheet", "SecurityError")) {
  return {
    get cssRules(): CSSRuleList {
      throw error;
    },
  };
}

const sheet = (value: object) => value as CSSStyleSheet;
const media = (condition = "(prefers-color-scheme: dark)") => group([], condition).node;

describe("original sender stylesheet scheme filtering", () => {
  it("skips an opaque sheet and still filters all readable sibling sheets", () => {
    const first = group([{}, media(), media()]);
    const last = group([media("(width > 600px)"), media()]);
    expect(() =>
      dropSchemeVariants({ styleSheets: [sheet(first.node), sheet(opaque()), sheet(last.node)] }),
    ).not.toThrow();
    expect(first.rules).toEqual([{}]);
    expect(last.rules).toHaveLength(1);
    expect(last.rules[0]).toMatchObject({ conditionText: "(width > 600px)" });
  });

  it("does not read a denied nested group outside its protected traversal", () => {
    const denied = { ...Object.getOwnPropertyDescriptors(opaque()) };
    const nested = Object.defineProperties({ conditionText: "(display: grid)" }, denied);
    const supported = group([media(), {}], "(display: grid)");
    const parent = group([media(), nested, supported.node, media()]);
    expect(() => dropSchemeVariants({ styleSheets: [sheet(parent.node)] })).not.toThrow();
    expect(parent.rules).toEqual([nested, supported.node]);
    expect(supported.rules).toEqual([{}]);
  });

  it("recognises an iframe SecurityError without relying on instanceof", () => {
    const frame = document.createElement("iframe");
    document.body.append(frame);
    try {
      const other = frame.contentWindow as Window & typeof globalThis;
      const error = new other.DOMException("Cross-origin stylesheet", "SecurityError");
      expect(error).not.toBeInstanceOf(DOMException);
      expect(() => dropSchemeVariants({ styleSheets: [sheet(opaque(error))] })).not.toThrow();
    } finally {
      frame.remove();
    }
  });

  it("does not swallow unrelated failures", () => {
    const error = new Error("unexpected CSSOM failure");
    expect(() => dropSchemeVariants({ styleSheets: [sheet(opaque(error))] })).toThrow(error);
  });

  it("keeps mounting and reusing original HTML when a sheet is opaque", () => {
    const host = document.createElement("div");
    const root = host.attachShadow({ mode: "open" });
    Object.defineProperty(root, "styleSheets", { value: [sheet(opaque())] });
    expect(() => mountOriginal(host, '<p data-sender="first">First message</p>')).not.toThrow();
    expect(host.shadowRoot).toBe(root);
    expect(root.querySelector("[data-sender]")?.textContent).toBe("First message");
    expect(() => mountOriginal(host, '<p data-sender="second">Second message</p>')).not.toThrow();
    expect(root.querySelector("[data-sender]")?.textContent).toBe("Second message");
  });

  it("allows roots without stylesheets", () => {
    expect(() => dropSchemeVariants({})).not.toThrow();
    expect(() => dropSchemeVariants({ styleSheets: null })).not.toThrow();
  });
});
