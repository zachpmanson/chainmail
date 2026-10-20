import { createElement, useLayoutEffect } from "react";
import { act, create } from "react-test-renderer";
import { describe, expect, it } from "vitest";
import useScopedSelection from "../src/lib/inbox/useScopedSelection";

describe("scope-owned selection without remounting", () => {
  it("never commits old ticks under a new scope", () => {
    const commits: { scope: string; chosen: string[] }[] = [];
    let current: ReturnType<typeof useScopedSelection>;
    function Observe({ scope, chosen }: { scope: string; chosen: string[] }) {
      useLayoutEffect(() => {
        commits.push({ scope, chosen });
      });
      return null;
    }
    function Probe({ scope }: { scope: string }) {
      current = useScopedSelection(scope);
      return createElement(Observe, { scope, chosen: current.chosen });
    }
    let rendered: ReturnType<typeof create>;
    act(() => {
      rendered = create(createElement(Probe, { scope: "work/INBOX" }));
    });
    try {
      act(() => {
        current.toggle("S");
      });
      expect(current!.chosen).toEqual(["S"]);
      commits.length = 0;
      act(() => {
        rendered!.update(createElement(Probe, { scope: "personal/INBOX" }));
      });
      expect(commits).toEqual([{ scope: "personal/INBOX", chosen: [] }]);
      act(() => {
        current.toggle("T");
      });
      act(() => {
        rendered!.update(createElement(Probe, { scope: "work/INBOX" }));
      });
      expect(current!.chosen).toEqual([]);
    } finally {
      act(() => {
        rendered!.unmount();
      });
    }
  });

  it("ignores callbacks retained from the previous scope", () => {
    let current: ReturnType<typeof useScopedSelection>;
    function Probe({ scope }: { scope: string }) {
      current = useScopedSelection(scope);
      return null;
    }
    let rendered: ReturnType<typeof create>;
    act(() => {
      rendered = create(createElement(Probe, { scope: "work/INBOX" }));
    });
    try {
      const previous = current!;
      act(() => {
        rendered!.update(createElement(Probe, { scope: "personal/INBOX" }));
      });
      act(() => {
        current.toggle("T");
      });
      act(() => {
        previous.toggle("S");
        previous.clearChosen();
      });
      expect(current!.chosen).toEqual(["T"]);
    } finally {
      act(() => {
        rendered!.unmount();
      });
    }
  });
});
