// @vitest-environment jsdom
import { createElement as h } from "react";
import { expect, it, vi } from "vitest";
import DraftCards from "../src/components/compose/DraftCards";
import {
  DraftSelectionContext,
  type DraftCoordinator,
} from "../src/components/compose/DraftSelectionContext";
import { draftCardSummary } from "../src/lib/ui/draftCardSummary";
import type { SavedDraft } from "../src/lib/ui/draftSelection";
import { domHarness, pick } from "./domHarness";

const render = domHarness();
const own: SavedDraft = {
  accountId: "A",
  id: "own",
  kind: "compose",
  resolution: "compose",
  subject: "Current draft",
  body: "Opening words",
};
const other: SavedDraft = { ...own, id: "other", subject: "Other draft" };
function coordinator(id: string, loading = false): DraftCoordinator {
  return {
    selection: { epoch: 1, active: true, loading, accountId: "A", id },
    editor: { current: undefined },
    select: vi.fn(),
    clear: vi.fn(),
    detach: () => true,
    approve: () => true,
    retire: vi.fn(),
    isRetired: () => false,
  };
}
function cards({
  drafts = [own, other],
  editingId,
  selected,
  onSelect = vi.fn(),
  loading = false,
}: {
  drafts?: SavedDraft[];
  editingId?: string;
  selected?: DraftCoordinator;
  onSelect?: (draft: SavedDraft) => void;
  loading?: boolean;
} = {}) {
  return h(
    DraftSelectionContext.Provider,
    { value: selected ?? null },
    h(DraftCards, { accountId: "A", drafts, loading, editingId, onSelect }),
  );
}

it.each([undefined, "", " \n\t "])("counts empty bodies as zero (%s)", (body) => {
  expect(draftCardSummary(body)).toEqual({ preview: "", wordCount: 0 });
});
it("normalizes preview whitespace and counts the full body", () => {
  expect(draftCardSummary("  One\n two\tthree  ")).toEqual({
    preview: "One two three",
    wordCount: 3,
  });
  const words = Array.from({ length: 30 }, (_, i) => `word${i}`);
  expect(draftCardSummary(words.join(" "))).toEqual({
    preview: words.slice(0, 12).join(" ") + "…",
    wordCount: 30,
  });
});
it("bounds even a single long token without cutting a surrogate pair", () => {
  const summary = draftCardSummary("😀".repeat(150));
  expect(summary).toEqual({ preview: "😀".repeat(100) + "…", wordCount: 1 });
});
it("keeps complete short openings without an ellipsis", () => {
  expect(draftCardSummary("Hello")).toEqual({ preview: "Hello", wordCount: 1 });
});
it("renders only the opening and total count but opens the original full draft", async () => {
  const body = [...Array.from({ length: 100 }, (_, i) => `word${i}`), "HIDDEN_TAIL"].join(" ");
  const draft = Object.freeze({ ...own, body });
  const onSelect = vi.fn();
  const host = await render(cards({ drafts: [draft], onSelect }));
  expect(host.textContent).toContain("word0 word1 word2");
  expect(host.textContent).toContain("… · 101 words");
  expect(host.textContent).not.toContain("word12");
  expect(host.textContent).not.toContain("HIDDEN_TAIL");
  await pick(host.querySelector("button")!);
  expect(onSelect).toHaveBeenCalledOnce();
  expect(onSelect.mock.calls[0]![0]).toBe(draft);
  expect(draft.body).toBe(body);
});
it.each([
  ["", "0 words"],
  ["Hello", "1 word"],
])("renders a readable count for %s", async (body, label) => {
  const host = await render(cards({ drafts: [{ ...own, body }] }));
  expect(host.textContent).toContain(label);
});
it("hides other cards for an owned editor and restores them when editing ends", async () => {
  let host = await render(cards());
  expect(host.querySelectorAll("[data-draft]")).toHaveLength(2);
  host = await render(cards({ editingId: own.id }));
  expect(host.querySelectorAll("[data-draft]")).toHaveLength(1);
  expect(host.textContent).toContain("Current draft");
  expect(host.textContent).not.toContain("Other draft");
  host = await render(cards());
  expect(host.querySelectorAll("[data-draft]")).toHaveLength(2);
});
it("matches the current editor by account as well as opaque draft ID", async () => {
  const foreign = { ...own, accountId: "B", subject: "Foreign account draft" };
  const host = await render(cards({ drafts: [own, foreign], editingId: own.id }));
  expect(host.querySelectorAll("[data-draft]")).toHaveLength(1);
  expect(host.textContent).not.toContain("Foreign account draft");
});
it("keeps only a selected draft even before the editor has adopted its ID", async () => {
  const host = await render(cards({ editingId: own.id, selected: coordinator(other.id) }));
  expect(host.querySelectorAll("[data-draft]")).toHaveLength(1);
  expect(host.textContent).toContain("Other draft");
  expect(host.textContent).toContain("Selected");
  expect(host.textContent).not.toContain("Current draft");
});
it("preserves pending selection and its clear escape hatch without exposing other cards", async () => {
  const selected = coordinator("pending", true);
  const host = await render(cards({ editingId: own.id, selected }));
  expect(host.querySelectorAll("[data-draft]")).toHaveLength(1);
  expect(host.querySelector('[data-draft="pending"]')?.getAttribute("aria-busy")).toBe("true");
  expect(host.textContent).toContain("Loading selected draft");
  expect(host.textContent).not.toContain("Current draft");
  expect(host.textContent).not.toContain("Other draft");
  await pick(host.querySelector("button")!);
  expect(selected.clear).toHaveBeenCalledOnce();
});
it("hides choices rather than falsely reporting no saved drafts when an owned draft is absent from a refetch", async () => {
  const host = await render(cards({ drafts: [other], editingId: own.id }));
  expect(host.querySelector('[aria-label="Saved Gmail drafts"]')).toBeNull();
});
