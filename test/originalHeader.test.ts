// @vitest-environment jsdom
import { createElement } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, describe, expect, it, vi } from "vitest";
import Message from "../src/components/thread/Message";
import type { OriginalAnalysis, OriginalPayload } from "../src/lib/message/original";

let tree: ReactTestRenderer | undefined;
afterEach(async () => {
  await act(async () => tree?.unmount());
  tree = undefined;
});
const designed =
  '<div class="a"><table style="color:red"><tr><td><table class="b"><tr><td>News</td></tr></table></td></tr></table></div>';
async function render(
  html: string,
  explicit?: boolean,
  supplied?: OriginalAnalysis | null,
  pending?: Promise<OriginalPayload>,
) {
  const analysis =
    supplied === null
      ? undefined
      : (supplied ?? {
          score: html === designed ? 2 : 0,
          matches:
            html === designed
              ? [
                  { rule: "bounded or padded main layout", points: 1 },
                  { rule: "designed action", points: 1 },
                ]
              : [],
          defaultsToOriginal: html === designed,
          uncertain: false,
        });
  const load = vi.fn(async () => pending ?? { html, analysis });
  await act(async () => {
    tree = create(
      createElement(Message, {
        email: {
          sender: "Ada",
          fromEmail: "header@example.test",
          body: "<p>Transcript</p>",
          stamp: { date: "21 Oct 2026", zone: "unknown" },
        },
        place: { id: "header" },
        look: { orgSlot: "" },
        reading: {
          original: { extId: "header", load },
          person: { id: 1, preferOriginal: explicit },
          onPreferOriginal: vi.fn(),
        },
      }),
    );
  });
  return load;
}
function explanation() {
  // The receipt is inside native <details>, not its always-visible <summary>.
  return tree!.root
    .findByType("details")
    .findByProps({ "data-original-analysis": true })
    .children.join("");
}

describe("expanded message header", () => {
  it("shows the matched rules, total and automatic selection", async () => {
    await render(designed);
    expect(explanation()).toBe(
      "HTML 2 pt — bounded or padded main layout (+1), designed action (+1) · automatic original (main layout + design evidence required)",
    );
    expect(
      tree!.root.findByType("summary").findAllByProps({ "data-original-analysis": true }),
    ).toHaveLength(0);
  });

  it("clearly explains zero/no matches", async () => {
    await render("<p>Hello</p>");
    expect(explanation()).toBe(
      "HTML 0 pt — no matches · automatic stripped (main layout + design evidence required)",
    );
  });

  it("loads a score on expansion while preserving the saved stripped view", async () => {
    const load = await render(designed, false);
    expect(load).not.toHaveBeenCalled();
    await act(async () =>
      tree!.root.findByType("details").props.onToggle({ currentTarget: { open: true } }),
    );
    expect(load).toHaveBeenCalledTimes(1);
    expect(explanation()).toBe(
      "HTML 2 pt — bounded or padded main layout (+1), designed action (+1) · saved stripped view (overrides score)",
    );
    expect(tree!.root.findAllByProps({ "data-original": true })).toHaveLength(0);
  });

  it("shows the server receipt instead of inspecting distinct display HTML", async () => {
    await render("<p>Ordinary display</p>", undefined, {
      score: 2,
      matches: [{ rule: "server rule", points: 2 }],
      defaultsToOriginal: true,
      uncertain: false,
    });
    expect(explanation()).toBe(
      "HTML 2 pt — server rule (+2) · automatic original (main layout + design evidence required)",
    );
  });

  it("distinguishes unavailable analysis from zero", async () => {
    await render(designed, undefined, null);
    expect(explanation()).toBe("HTML score unavailable");
  });

  it("shows pending and failed analysis without fabricating a receipt", async () => {
    let reject!: (error: Error) => void;
    const pending = new Promise<OriginalPayload>((_yes, no) => {
      reject = no;
    });
    await render(designed, undefined, undefined, pending);
    expect(explanation()).toBe("HTML score loading…");
    await act(async () => reject(new Error("not available")));
    expect(explanation()).toBe("HTML score unavailable");
  });

  it("explains uncertainty even with enough design evidence", async () => {
    await render(designed, undefined, {
      score: 2,
      matches: [{ rule: "main design", points: 2 }],
      defaultsToOriginal: false,
      uncertain: true,
    });
    expect(explanation()).toBe(
      "HTML 2 pt — main design (+2) · automatic stripped (analysis uncertain)",
    );
  });

  it("labels a saved original override even with no matches", async () => {
    await render("<p>Hello</p>", true);
    expect(explanation()).toBe("HTML 0 pt — no matches · saved original view (overrides score)");
  });
});
