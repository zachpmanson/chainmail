import type { CorpusEntry } from "./api";

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

/** The mailbox id inside a permalink, when the link is one: Gmail's opens a
 *  message at .../#all/<id>. Everything else — a Slack permalink, a link into a
 *  self-hosted archive — is not a Gmail id and gets none rather than a guess. */
export function gmailIdOf(e: CorpusEntry): string | undefined {
  const link = e.permalink ?? "";
  const m = /#all\/([^/?#]+)$/.exec(link);
  return m ? m[1] : undefined;
}

/**
 * The provenance line for one entry of a thread, in the same shape a page build
 * writes: `msg <gmail-id>` for a message the mailbox holds, `unspooled from msg
 * <host>, msg <host>` for one that exists only inside somebody's quote, and the
 * corpus ext id where no mailbox id is known.
 *
 * This mirrors the generator (internal/spec's builder.source) rather than asking
 * the API for the line, because the wire has no field that means this: an entry's
 * `source` says which archive it came from (mail), and the ids are carried as
 * sightings. `provenance()` parses the generated shape and treats anything else
 * as prose, so the shape IS the contract between the two producers — and a second
 * producer is what lets a reader who saw a receipt on a built page see the same
 * receipt in the pane. The lockstep is pinned by a test on each side.
 *
 * `nameOf` names the message a quote was recovered from, because the useful name
 * for a host is the host's own mailbox id and only the caller knows which entries
 * it holds. A host this producer cannot name — one whose row is not in the thread
 * — is named by its ext id rather than dropped, which is where it parts company
 * with the generator: a built page silently omits a host it has no row for, while
 * the id is what a pane's reader can quote even when the message is not on the
 * page. The one case the two agree on is a recovery with no host at all, which
 * says so in words.
 */
export function sourceLine(e: CorpusEntry, nameOf: (extId: string) => string): string {
  // `quoted` is the corpus's own word for "exists only because somebody quoted
  // it", which is the same fact the generator branches on (its row's Direct),
  // and it is a field rather than a reading of the sightings: an entry whose
  // sightings were not sent must not be described as a recovery.
  if (!e.quoted) {
    const gmail = gmailIdOf(e);
    return gmail ? `msg ${gmail}` : e.extId;
  }
  const named: string[] = [];
  for (const s of e.sightings ?? []) {
    if (s.kind === "direct" || !s.seenIn) continue;
    const name = nameOf(s.seenIn);
    // A sighting is keyed by (entry, host, kind), so a host that both quoted and
    // forwarded an entry arrives twice: one message to open, not two.
    if (!named.includes(name)) named.push(name);
  }
  if (named.length) return `unspooled from ${named.join(", ")}`;
  return "unspooled from quoted text";
}

