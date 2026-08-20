import type { Entry, Timeline } from "./spec";
import { entryId, initials } from "./anchors";
import { order, zones, type Zones } from "./chronological";
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
  stamp: { date: string; time?: string; tz?: string; inferred: boolean };
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
  orgs: string[];
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

  // colour slot per org, in first-appearance order
  const orgs: string[] = [];
  for (const e of ordered) if (e.org && !orgs.includes(e.org)) orgs.push(e.org);
  const slot = (org?: string) => (org ? `o${Math.min(orgs.indexOf(org) + 1, 5)}` : "o5");

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
      stamp: { date: entry.date, time: entry.time, tz: lbl.tz, inferred: lbl.inferred },
    };
  });

  const title = (input.title ?? "Timeline").replace(/^#+/, (m) => (m ? "#" : ""));
  return {
    spec, rows, layout: lay, zones: z, orgs, title, avatarCss,
    hashed: title.startsWith("#"),
    whoTitle: (name) => (emails.has(name) ? `${name} <${emails.get(name)}>` : name),
  };
}

export { initials };
