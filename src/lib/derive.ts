import type { Entry, Timeline } from "./spec";
import { entryId, initials } from "./anchors";
import { order, zones, type ZoneState, type Zones } from "./chronological";
import { layout, type Layout } from "./lanes";
import { editHtml } from "./editDiff";

/** A quoter's edit resolved for the bubble: diff markup plus attribution. */
export interface RowEdit {
  /** spec id of the original message the change was made to (anchor target) */
  base: string;
  who: string;
  time: string;
  /** the quoter's modified text as diff-marked HTML (`.edel` strike / `.eins` insert) */
  html: string;
}

export interface Row {
  entry: Entry;
  id: string;
  row: number;
  lane: number;
  chain: string;
  isChainStart: boolean;
  orgSlot: string;
  /** class suffix for this sender's avatar image, e.g. "p0"; absent = initials */
  avatarClass?: string;
  /** the quoter's inline edits to this message's quoted text, resolved to diff markup */
  edits?: RowEdit[];
  stamp: { date: string; time?: string; tz?: string; zone: ZoneState };
}

export interface View {
  spec: Timeline;
  /** one rule per avatar, so a data: URI is emitted once however many messages
   *  that person sent. Inlining it per element multiplied a 19 KB face by 27. */
  avatarCss: string;
  rows: Row[];
  layout: Layout;
  zones: Zones;
  /** name -> "Name <address>" for hover titles; address omitted when unknown */
  whoTitle: (name: string) => string;
  /** orgs in first-appearance order. This is the colour-slot order: a message's
   *  and an avatar's slot is this array's index, so the order is load-bearing
   *  even though nothing renders the list itself. */
  orgs: string[];
  /** the slot class for an org, e.g. "o2". The panel and the transcript both go
   *  through this, so a person's row and their bubbles cannot be coloured
   *  differently. An unknown org takes "o5", which a fifth org also takes. */
  orgSlot: (org?: string) => string;
  title: string;
  hashed: boolean;
}

/** Everything the components need, computed once. */
export function derive(input: Timeline): View {
  const used = new Set<string>();
  const idMap = new Map<Entry, string>(input.messages.map((e) => [e, entryId(e, used)]));
  const idOf = (e: Entry) => idMap.get(e)!;
  const byId = new Map<string, Entry>();
  for (const [e, id] of idMap) byId.set(id, e);

  // A quoter's edit to a quoted message (issue #42) is drawn inside the message
  // that quoted it, never as its own floating node. The backend already attaches
  // the edit to the host and carries its own id; here we drop the derived entry
  // from the laid-out rows and let the host bubble render it inline.
  //
  // A copy is hoisted however many replies it has, because each of those replies is
  // anchored to the message the edit is diffed against, not to this ghost of it. A
  // real chain (the Termina x Ruralco CSV thread) has the host — the message that
  // re-quotes and therefore carries the edit — as a DIRECT descendant of the
  // edited copy, because the thread was replied-to from the copied row before the
  // corpus realised it was a derivative. Re-parenting those replies up to the
  // copy's own parent (the base) keeps the subtree attached and the reply graph
  // intact, instead of leaving the host orphaned on a dangling id.
  const hoisted = new Set<string>();
  for (const e of input.messages) {
    for (const ed of e.edits ?? []) if (ed.id && byId.has(ed.id)) hoisted.add(ed.id);
  }
  // A hoisted copy is replaced in every remaining parent chain by its own parent
  // (the base it derives from), so descendants attach to what the copy itself
  // attached to, rather than dangling on an id that no longer occupies a row.
  // Mutated in place: idOf keys by object identity, so a clone would be a
  // stranger to the id map and every layout function after this reads parent off
  // the very entries it is given.
  const effective = (id?: string): string | undefined => {
    const seen = new Set<string>();
    let cur = id;
    while (cur && byId.has(cur) && hoisted.has(cur) && !seen.has(cur)) {
      seen.add(cur);
      cur = byId.get(cur)!.parent;
    }
    return cur && byId.has(cur) ? cur : undefined;
  };
  const visible = input.messages.filter((e) => !hoisted.has(idOf(e)));
  for (const e of visible) e.parent = effective(e.parent);

  const ordered = order(visible, idOf);
  const spec: Timeline = { ...input, messages: ordered as Timeline["messages"] };
  const lay = layout(ordered, idOf);
  const z = zones(ordered);

  // Colour slot per org, in first-appearance order. The messages come first and
  // the panel's orgs are appended after them, never interleaved: appending cannot
  // move an org the transcript already placed, so adding a cc-only recipient
  // cannot repaint the page. Deriving one order over both together would let the
  // panel decide what colour a bubble is.
  //
  // The panel's orgs are here at all because a recipient who sent nothing can be
  // at an org no message came from. Leaving them out would put a whole org on the
  // unknown grey — indistinguishable from a person whose org nothing established.
  const orgs: string[] = [];
  for (const e of ordered) if (e.org && !orgs.includes(e.org)) orgs.push(e.org);
  for (const p of input.participants ?? []) if (p.org && !orgs.includes(p.org)) orgs.push(p.org);
  // An org absent from the list indexes to -1 and would name the slot "o0", a
  // class with no colour behind it, so it is folded into the unknown slot rather
  // than rendering as nothing at all.
  const slot = (org?: string) => {
    const i = org ? orgs.indexOf(org) : -1;
    return i < 0 ? "o5" : `o${Math.min(i + 1, 5)}`;
  };

  const avatarNames = Object.keys(input.avatars ?? {}).sort();
  const avatarClass = new Map(avatarNames.map((n, i) => [n, `p${i}`]));
  const avatarCss = avatarNames
    .map((n) => `.av.${avatarClass.get(n)}{background-image:url(${input.avatars![n]})}`)
    .join("");

  const emails = new Map<string, string>();
  for (const e of ordered) if (e.sender && e.fromEmail && !emails.has(e.sender)) emails.set(e.sender, e.fromEmail);
  for (const p of spec.participants ?? []) if (p.email) emails.set(p.name, p.email);

  const firstRow = new Map(lay.chains.map((c) => [c.root, c.firstRow]));
  const laneOf = new Map(lay.chains.map((c) => [c.root, c.lane]));

  const rows: Row[] = ordered.map((entry) => {
    const id = idOf(entry);
    const chain = lay.chainOf.get(id)!;
    const lbl = z.label(entry);
    const edits = (entry.edits ?? [])
      .map((ed) => {
        const baseEntry = ed.base ? byId.get(ed.base) : undefined;
        return {
          base: ed.base ?? "",
          who: ed.who ?? entry.sender ?? "",
          time: ed.time ?? entry.time ?? "",
          html: editHtml(baseEntry?.body ?? "", ed.body ?? ""),
        };
      });
    return {
      entry, id, chain,
      row: lay.row.get(id)!,
      lane: laneOf.get(chain)!,
      isChainStart: lay.row.get(id) === firstRow.get(chain),
      orgSlot: slot(entry.org),
      avatarClass: entry.sender ? avatarClass.get(entry.sender) : undefined,
      edits: edits.length ? edits : undefined,
      stamp: { date: entry.date, time: entry.time, tz: lbl.tz, zone: lbl.state },
    };
  });

  const title = (input.title ?? "Timeline").replace(/^#+/, (m) => (m ? "#" : ""));
  return {
    spec, rows, layout: lay, zones: z, orgs, orgSlot: slot, title, avatarCss,
    hashed: title.startsWith("#"),
    whoTitle: (name) => (emails.has(name) ? `${name} <${emails.get(name)}>` : name),
  };
}

export { initials };
