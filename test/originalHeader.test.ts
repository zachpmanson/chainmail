// @vitest-environment jsdom
import { createElement } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, describe, expect, it, vi } from "vitest";
import Message from "../src/components/thread/Message";

let tree: ReactTestRenderer | undefined;
afterEach(async () => {
  await act(async () => tree?.unmount());
  tree = undefined;
});
const designed =
  '<div class="a"><table style="color:red"><tr><td><table class="b"><tr><td>News</td></tr></table></td></tr></table></div>';
async function render(html: string, explicit?: boolean) {
  const load = vi.fn(async () => html);
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
      "HTML 3 pt — nested tables (+2), 3+ styled elements (+1) · automatic original (≥3 pt for original)",
    );
    expect(
      tree!.root.findByType("summary").findAllByProps({ "data-original-analysis": true }),
    ).toHaveLength(0);
  });

  it("clearly explains zero/no matches", async () => {
    await render("<p>Hello</p>");
    expect(explanation()).toBe("HTML 0 pt — no matches · automatic stripped (≥3 pt for original)");
  });

  it("loads a score on expansion while preserving the saved stripped view", async () => {
    const load = await render(designed, false);
    expect(load).not.toHaveBeenCalled();
    await act(async () =>
      tree!.root.findByType("details").props.onToggle({ currentTarget: { open: true } }),
    );
    expect(load).toHaveBeenCalledTimes(1);
    expect(explanation()).toBe(
      "HTML 3 pt — nested tables (+2), 3+ styled elements (+1) · saved stripped view (overrides score)",
    );
    expect(tree!.root.findAllByProps({ "data-original": true })).toHaveLength(0);
  });

  it("labels a saved original override even with no matches", async () => {
    await render("<p>Hello</p>", true);
    expect(explanation()).toBe("HTML 0 pt — no matches · saved original view (overrides score)");
  });
});
