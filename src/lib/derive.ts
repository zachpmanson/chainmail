import type { Entry, Timeline } from "./spec";
import { entryId, initials } from "./anchors";
import { order, zones, type ZoneState, type Zones } from "./chronological";
import { layout, type Layout } from "./lanes";

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

  const ordered = order(input.messages, idOf);
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
    return {
      entry, id, chain,
      row: lay.row.get(id)!,
      lane: laneOf.get(chain)!,
      isChainStart: lay.row.get(id) === firstRow.get(chain),
      orgSlot: slot(entry.org),
      avatarClass: entry.sender ? avatarClass.get(entry.sender) : undefined,
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
