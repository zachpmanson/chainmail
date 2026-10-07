import type { Entry, Timeline } from "./spec";
import { normalise } from "./normalise";
import { entryId } from "./anchors";

export type Mark = "new" | "revised";

export function extractSpec(pageHtml: string): Timeline {
  // `mt-spec` is the old Python renderer's id.
  const m =
    /<script type="application\/json" id="(?:chainmail-spec|mt-spec)">([\s\S]*?)<\/script>/.exec(
      pageHtml,
    );
  if (!m) {
    throw new Error(
      "no embedded spec in that page — it was not produced by chainmail or its predecessor, " +
        "so there is nothing to reload",
    );
  }
  return normalise(JSON.parse(m[1]!.replace(/<\\\//g, "</")));
}

const ids = (entries: Entry[]) => {
  const used = new Set<string>();
  return new Map(entries.map((e) => [e, entryId(e, used)]));
};

/** Identity of an entry's *content*, for spotting edits under an unchanged anchor. */
const contentKey = (e: Entry) => `${e.body} ${e.sender ?? ""} ${e.label ?? ""}`;

/** Anchors derive from date+time+sender, so a corrected timestamp falls back to a
 *  body match: revised, not deleted+new. */
export function diff(prev: Timeline, next: Timeline): Map<string, Mark> {
  const prevIds = ids(prev.messages);
  const nextIds = ids(next.messages);
  const prevById = new Map([...prevIds].map(([e, id]) => [id, contentKey(e)]));
  const prevBodies = new Set([...prevIds.keys()].map(contentKey));

  const marks = new Map<string, Mark>();
  for (const [entry, id] of nextIds) {
    const key = contentKey(entry);
    if (prevById.has(id)) {
      if (prevById.get(id) !== key) marks.set(id, "revised");
    } else if (prevBodies.has(key)) {
      marks.set(id, "revised"); // same words, moved or re-timed
    } else {
      marks.set(id, "new");
    }
  }
  return marks;
}
