// @vitest-environment jsdom
import { act, createElement as h } from "react";
import { describe, expect, it, vi } from "vitest";
import FullThreadRow from "../src/components/inbox/FullThreadRow";
import CompactThreadRow from "../src/components/inbox/CompactThreadRow";
import ThreadListHeader from "../src/components/inbox/ThreadListHeader";
import useScopedSelection from "../src/lib/inbox/useScopedSelection";
import { useEscapeToClear } from "../src/lib/inbox/selection";
import { domHarness, key, settle } from "./domHarness";

vi.mock("../src/lib/inbox/useAccountId", () => ({ useAccountId: () => "A" }));
vi.mock("../src/lib/inbox/mailActions", () => ({
  useReadAction: () => ({ isPending: false, mutate: vi.fn() }),
  useMailAction: () => ({ isPending: false, mutate: vi.fn() }),
}));
vi.mock("../src/lib/ui/useOwnToast", () => ({ default: () => vi.fn() }));
const render = domHarness();
const open = vi.fn();
function List({ compact, scope = "A/INBOX" }: { compact: boolean; scope?: string }) {
  const ids = ["A", "B", "C", "D"];
  const selection = useScopedSelection(scope, ids);
  useEscapeToClear(selection.canClear, selection.clearChosen);
  const Row = compact ? CompactThreadRow : FullThreadRow;
  return h(
    "div",
    null,
    h(ThreadListHeader, {
      compact,
      checked: selection.allSelected,
      mixed: selection.mixed,
      onChange: selection.toggleAll,
    }),
    h(
      "ul",
      null,
      ids.map((id) =>
        h(Row, {
          key: id,
          thread: {
            rootExtId: id,
            subject: id,
            unread: 1,
            entries: 1,
            matched: 1,
            people: 1,
            attachments: 0,
            first: "2026-01-01T00:00:00Z",
            last: "2026-01-01T00:00:00Z",
            score: 1,
          },
          current: false,
          checked: selection.chosen.includes(id),
          onToggle: (extend, checked) => selection.toggle(id, extend, checked),
          onOpen: open,
        }),
      ),
    ),
  );
}
async function click(input: Element, shiftKey = false) {
  await act(async () => input.dispatchEvent(new MouseEvent("click", { bubbles: true, shiftKey })));
  await settle();
}
const row = (host: HTMLElement, id: string) =>
  host.querySelector<HTMLInputElement>(`[data-root="${id}"] input`)!;
const selected = (host: HTMLElement) =>
  [...host.querySelectorAll<HTMLInputElement>("[data-root] input:checked")].map(
    (input) => input.closest<HTMLElement>("[data-root]")!.dataset.root,
  );

describe.each([false, true])("native range selection compact=%s", (compact) => {
  it("applies Shift clicks once and exposes a real mixed native header", async () => {
    open.mockClear();
    const host = await render(h(List, { compact }));
    const all = host.querySelector<HTMLInputElement>('[aria-label="Select all shown threads"]')!;
    expect(all.closest('[aria-hidden="true"]')).toBeNull();
    expect(all.closest("label")!.classList.contains("select-none")).toBe(true);
    expect(row(host, "A").closest("label")!.classList.contains("select-none")).toBe(true);
    expect(all.checked).toBe(false);
    expect(all.indeterminate).toBe(false);
    await click(row(host, "A").closest("label")!);
    expect(selected(host)).toEqual(["A"]);
    expect(all.indeterminate).toBe(true);
    await click(row(host, "C"), true);
    expect(selected(host)).toEqual(["A", "B", "C"]);
    await click(all);
    expect(selected(host)).toEqual(["A", "B", "C", "D"]);
    expect(all.checked).toBe(true);
    expect(all.indeterminate).toBe(false);
    await click(row(host, "B"), true);
    expect(selected(host)).toEqual(["A", "C", "D"]);
    expect(all.indeterminate).toBe(true);
    await click(all);
    await click(all);
    expect(selected(host)).toEqual([]);
    expect(all.indeterminate).toBe(false);
    expect(open).not.toHaveBeenCalled();
  });
  it("Escape clears an unchecked anchor before a subsequent Shift click", async () => {
    const host = await render(h(List, { compact }));
    await click(row(host, "A"));
    await click(row(host, "A"));
    expect(selected(host)).toEqual([]);
    await key(row(host, "A"), "Escape");
    await click(row(host, "D"), true);
    expect(selected(host)).toEqual(["D"]);
  });
  it("scope changes clear the range anchor without remounting the list", async () => {
    const host = await render(h(List, { compact }));
    await click(row(host, "A"));
    await render(h(List, { compact, scope: "B/INBOX" }));
    expect(selected(host)).toEqual([]);
    await click(row(host, "D"), true);
    expect(selected(host)).toEqual(["D"]);
  });
});
