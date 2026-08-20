import type { Entry, Timeline } from "./spec";
import { normalise } from "./normalise";
import { entryId } from "./anchors";

export type Mark = "new" | "revised";

/**
 * Recover the spec embedded in a page chainmail rendered. Rendered HTML is lossy
 * and messy to reparse; the embedded JSON is the exact input, so feeding a prior
 * run back in is a clean round-trip rather than a scrape.
 */
export function extractSpec(pageHtml: string): Timeline {
  // `mt-spec` is the id used by the Python renderer this replaced; pages built
  // by it are still worth reloading, and normalise() handles their snake_case.
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

/**
 * What this pass changed relative to a previous one.
 *
 * Anchors derive from date+time+sender, so a corrected timestamp changes an id and
 * would otherwise read as "one deleted, one new"; the body is matched as a
 * fallback so it reports as *revised* instead.
 */
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
