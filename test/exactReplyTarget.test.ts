import { createElement as h } from "react";
import { act, create } from "react-test-renderer";
import { expect, it } from "vitest";
import type { CorpusEntry } from "../src/lib/api/api";
import { useReplyTarget } from "../src/lib/thread/useReplyTarget";
const older = {
  extId: "older",
  permalink: "https://mail.invalid/#all/old",
  source: "mail",
  quoted: false,
  ts: "2026-01-01T00:00:00Z",
} as CorpusEntry;
const newer = {
  extId: "newer",
  permalink: "https://mail.invalid/#all/new",
  source: "mail",
  quoted: false,
  ts: "2026-01-02T00:00:00Z",
} as CorpusEntry;
it("resolves a hidden exact parent from raw entries and never falls back on missing/unanswerable parents", async () => {
  let answer: CorpusEntry | undefined;
  function Probe({ parent, entries }: { parent: string; entries: CorpusEntry[] }) {
    answer = useReplyTarget("root", [newer], { parent, entries }).answer;
    return null;
  }
  let root!: ReturnType<typeof create>;
  await act(async () => {
    root = create(h(Probe, { parent: "older", entries: [older, newer] }));
  });
  expect(answer?.extId).toBe("older");
  await act(async () => root.update(h(Probe, { parent: "missing", entries: [older, newer] })));
  expect(answer).toBeUndefined();
  await act(async () =>
    root.update(h(Probe, { parent: "older", entries: [{ ...older, permalink: "" }, newer] })),
  );
  expect(answer).toBeUndefined();
  await act(async () => root.unmount());
});
