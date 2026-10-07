import { readFileSync } from "node:fs";
import type { CorpusEntry } from "../src/lib/api/api";
import { normalise } from "../src/lib/timeline/normalise";
import { entryId } from "../src/lib/timeline/anchors";
import type { Entry, Timeline } from "../src/lib/timeline/spec";

/** A spec from fixtures/, as the app loads one. */
export const load = (name: "synthetic" | "minimal"): Timeline =>
  normalise(JSON.parse(readFileSync(new URL(`../fixtures/${name}.json`, import.meta.url), "utf8")));

/** The id function derive.ts builds, over a fixed list of entries. */
export const idsFor = (entries: Entry[]): ((e: Entry) => string) => {
  const used = new Set<string>();
  const ids = new Map(entries.map((e) => [e, entryId(e, used)]));
  return (e) => ids.get(e)!;
};

export const corpus = (extId: string, extra: Partial<CorpusEntry> = {}): CorpusEntry => ({
  extId,
  source: "mail",
  quoted: false,
  ts: "2026-03-02T09:15:00Z",
  ...extra,
});

export const msg = (id: string, extra: Partial<Entry> = {}): Entry => ({
  kind: "message",
  id,
  date: "Mon 2 Mar 2026",
  body: `<p>${id}</p>`,
  ...extra,
});

export const timeline = (...messages: Entry[]): Timeline => ({
  title: "Test",
  messages: messages as Timeline["messages"],
});

/** How stampOf renders 2 Mar 2026 today; stamp.test.ts has an it.fails for the documented "Mon 2 Mar 2026". */
export const MON_2_MAR = "Mon, 2 Mar 2026";
