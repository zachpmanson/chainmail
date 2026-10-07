import type { CorpusEntry } from "../api/api";

/**
 * `entry.source` is free text by contract: only the generated "unspooled from msg <id>"
 * shape is parsed, anything else is rendered verbatim.
 */

/** One message the provenance line names. */
export interface SourceId {
  /** as written, e.g. "msg 1a2b3c4d5e6f7a8b" */
  text: string;
  /** the message to open; absent for a corpus ext id no mailbox holds */
  gmailId?: string;
}

export type Provenance =
  { kind: "prose"; text: string } | { kind: "ids"; prefix: string; ids: SourceId[] };

const UNSPOOLED = "unspooled from ";

// Not a Gmail-id character class: "msg " is the generator's promise, the id shape isn't.
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

export function msgCount(n: number): string {
  return `${n} msg${n === 1 ? "" : "s"}`;
}

/** Gmail permalinks open a message at .../#all/<id>; other links yield no id. */
export function gmailIdOf(e: CorpusEntry): string | undefined {
  const link = e.permalink ?? "";
  const m = /#all\/([^/?#]+)$/.exec(link);
  return m ? m[1] : undefined;
}

/**
 * Mirrors the generator's line shape (internal/spec), which provenance() parses;
 * a test on each side pins the lockstep. Unlike the generator, an unnamed host is kept.
 */
export function sourceLine(e: CorpusEntry, nameOf: (extId: string) => string): string {
  // Use `quoted`, not the sightings: unsent sightings must not read as a recovery.
  if (!e.quoted) {
    const gmail = gmailIdOf(e);
    return gmail ? `msg ${gmail}` : e.extId;
  }
  const named: string[] = [];
  for (const s of e.sightings ?? []) {
    if (s.kind === "direct" || !s.seenIn) continue;
    const name = nameOf(s.seenIn);
    // A host that both quoted and forwarded an entry has two sightings.
    if (!named.includes(name)) named.push(name);
  }
  if (named.length) return `unspooled from ${named.join(", ")}`;
  return "unspooled from quoted text";
}
