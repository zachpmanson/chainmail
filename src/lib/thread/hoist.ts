import type { CorpusEntry } from "../api/api";

/**
 * Hoists edited copies out (as derive.ts does) since their edit draws inside the quoter,
 * and re-points replies to a hoisted copy at the copy's own parent.
 */
export function hoistEdits(entries: CorpusEntry[]): {
  shown: CorpusEntry[];
  parentOf: Map<string, string | undefined>;
} {
  const at = new Map(entries.map((e) => [e.extId, e]));
  const hoisted = new Set<string>();
  for (const e of entries) {
    for (const ed of e.edits ?? []) if (ed.id && at.has(ed.id)) hoisted.add(ed.id);
  }
  const effective = (id?: string): string | undefined => {
    const seen = new Set<string>();
    let cur = id;
    while (cur && at.has(cur) && hoisted.has(cur) && !seen.has(cur)) {
      seen.add(cur);
      cur = at.get(cur)!.parent;
    }
    return cur && at.has(cur) ? cur : undefined;
  };
  const shown = entries.filter((e) => !hoisted.has(e.extId));
  return { shown, parentOf: new Map(shown.map((e) => [e.extId, effective(e.parent)])) };
}
