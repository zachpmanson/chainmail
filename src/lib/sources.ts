/**
 * The provenance line under a bubble — `entry.source` — resolved into something
 * the page can count and link.
 *
 * `source` is free text by contract: a generated spec writes
 * "unspooled from msg <id>, msg <id>", a hand-written one writes
 * "unspooled from the 2 Aug email". So this recognises the generated shape
 * rather than parsing prose, and returns anything else verbatim to be rendered
 * exactly as it was written. That is what keeps a human's gloss from being
 * mangled into a count it never claimed.
 *
 * The alternative — carrying the ids as a structured field on the entry — is a
 * better home for them, but `Entry` in schema/timeline.schema.json is
 * `additionalProperties: false`, so even an optional field is a change to a
 * published contract.
 */

/** One message the provenance line names. */
export interface SourceId {
  /** as written, e.g. "msg 1a2b3c4d5e6f7a8b" */
  text: string;
  /** the message to open; absent for a corpus ext id no mailbox holds */
  gmailId?: string;
}

export type Provenance =
  | { kind: "prose"; text: string }
  | { kind: "ids"; prefix: string; ids: SourceId[] };

const UNSPOOLED = "unspooled from ";

/**
 * A message the mailbox holds. Deliberately not a Gmail-id character class: the
 * "msg " prefix is what the generator promises, and pinning the handle's shape
 * to hex would silently stop linking the day a source hands out ids that look
 * different. The cost is that the prose "unspooled from msg two" would be read
 * as an id — nobody writes that, and TestSourceNamesEachHostAsMsgID pins the
 * only producer that writes this shape on purpose.
 */
const MSG = /^msg (\S+)$/;

/** A corpus ext id: 'mail:<message-id>' | 'slack:<ch>:<ts>' | 'quote:<sha>'. */
const EXT = /^(?:mail|slack|quote):\S+$/;

export function provenance(source: string): Provenance {
  const text = source.trim();
  const prefix = text.startsWith(UNSPOOLED) ? UNSPOOLED : "";
  const ids: SourceId[] = [];
  for (const part of text.slice(prefix.length).split(", ")) {
    const msg = MSG.exec(part);
    if (msg) {
      ids.push({ text: part, gmailId: msg[1] });
    } else if (EXT.test(part)) {
      ids.push({ text: part });
    } else {
      return { kind: "prose", text };
    }
  }
  return ids.length ? { kind: "ids", prefix, ids } : { kind: "prose", text };
}

/**
 * How many ids it takes before the line is worth collapsing.
 *
 * Two, because one id inline is strictly better than a click that reveals one
 * id, and because from two the summary is already shorter than what it replaces:
 * two Gmail handles run to about sixty characters against the twenty-one of
 * "unspooled from 2 msgs". Raising it to three would put a two-host line back on
 * the page at three times the width of the summary, buying the reader nothing
 * they could not get with one click.
 */
export const COLLAPSE_FROM = 2;

/**
 * "1 msg", not "1 msgs". The singular is unreachable from the collapse at the
 * current threshold — the participants panel and the thread list are what
 * exercise it — and it is stated here so moving the threshold cannot introduce a
 * grammar bug.
 */
export function msgCount(n: number): string {
  return `${n} msg${n === 1 ? "" : "s"}`;
}
