import type { CorpusEntry } from "../api/api";
import { orgOrder, slotsFor } from "../timeline/derive";
import { resolveEdits, type EditEntry, type RowEdit } from "../timeline/edits";
import { gmailIdOf } from "../message/sources";
import { senderTitle, withAddress, wordsOf } from "../message/who";
import { whenOf, type StampData } from "../ui/stamp";

/** Ext ids aren't valid HTML anchors, so anchors use the thread position. */
export const anchor = (i: number) => `entry-${i}`;

export type ThreadLookup = ReturnType<typeof threadLookup>;

/** `entries` includes hoisted copies; `shown` and `parentOf` come from hoistEdits. */
export function threadLookup(
  entries: CorpusEntry[],
  shown: CorpusEntry[],
  parentOf: Map<string, string | undefined>,
  people: Map<number, string[]>,
) {
  const slot = slotsFor(orgOrder(shown.map((e) => e.org)));
  // Recipients have no address in the chain read, so the identity graph fills in,
  // and wins over the entry's "address unknown; quoted by" wording.
  const titles = new Map<string, string>();
  for (const e of shown) if (e.author) titles.set(e.author, senderTitle(e));
  for (const e of shown)
    for (const p of e.participants ?? []) {
      const addresses = people.get(p.personId);
      if (addresses) titles.set(p.name, withAddress(p.name, addresses));
    }
  // Built from drawn entries only: a hoisted copy has no row to link to.
  const byExt = new Map<string, CorpusEntry>();
  const indexOf = new Map<string, number>();
  const anchorByGmail = new Map<string, string>();
  shown.forEach((e, i) => {
    byExt.set(e.extId, e);
    indexOf.set(e.extId, i);
    const gmail = gmailIdOf(e);
    if (gmail) anchorByGmail.set(gmail, anchor(i));
  });
  // Includes hoisted copies, which edits diff against.
  const everyExt = new Map(entries.map((e) => [e.extId, e]));

  const titleOf = (name: string): string => titles.get(name) ?? name;

  // Position in the corpus order, not draw order, so tree view doesn't rename anchors.
  const anchorOf = (extId: string): string => anchor(indexOf.get(extId) ?? 0);

  const mailName = (extId: string): string => {
    const host = byExt.get(extId);
    if (!host) return extId;
    const gmail = gmailIdOf(host);
    return gmail ? `msg ${gmail}` : host.extId;
  };

  // /v1/chains returns the whole thread, so a parent missing here isn't in the corpus.
  const replyOf = (e: CorpusEntry) => {
    const parentId = parentOf.get(e.extId);
    const parent = parentId ? byExt.get(parentId) : undefined;
    if (!parent) return null;
    const i = indexOf.get(parent.extId);
    if (i === undefined) return null;
    return { anchor: anchor(i), ...wordsOf(parent) };
  };

  // If the base was itself hoisted, the raw id stays and the link points nowhere.
  const editsOf = (e: CorpusEntry, at: StampData): RowEdit[] | undefined => {
    const resolved = resolveEdits(
      e.edits,
      (id) => {
        const copy = id ? everyExt.get(id) : undefined;
        if (!copy) return undefined;
        return {
          html: copy.html ?? "",
          who: copy.author ?? "",
          stamp: whenOf(copy),
        } satisfies EditEntry;
      },
      { who: e.author ?? "", time: at.time ?? "" },
    );
    for (const ed of resolved ?? []) {
      const i = indexOf.get(ed.base);
      if (i !== undefined) ed.base = anchor(i);
    }
    return resolved;
  };

  return { slot, titleOf, anchorOf, anchorByGmail, mailName, replyOf, editsOf };
}
