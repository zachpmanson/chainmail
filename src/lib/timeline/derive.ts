import type { Entry, Timeline } from "./spec";
import { entryId, initials } from "./anchors";
import { order, zones, type ZoneState, type Zones } from "./chronological";
import { layout, type Layout } from "./lanes";
import { resolveEdits, type EditEntry, type RowEdit } from "./edits";

export type { RowEdit } from "./edits";

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
  /** One rule per avatar, so each data: URI is emitted once rather than per message. */
  avatarCss: string;
  rows: Row[];
  layout: Layout;
  zones: Zones;
  /** name -> "Name <address>" for hover titles; address omitted when unknown */
  whoTitle: (name: string) => string;
  /** Colour-slot order: an org's slot is its index here. */
  orgs: string[];
  orgSlot: (org?: string) => string;
  title: string;
  hashed: boolean;
}

/** First sighting wins. Shared with the reading pane so both colour orgs the same way. */
export function orgOrder(values: (string | undefined)[]): string[] {
  const out: string[] = [];
  for (const v of values) if (v && !out.includes(v)) out.push(v);
  return out;
}

/** Unknown orgs share "o5" with the fifth org; "o0" would have no colour. */
export function slotsFor(orgs: string[]): (org?: string) => string {
  return (org?: string) => {
    const i = org ? orgs.indexOf(org) : -1;
    return i < 0 ? "o5" : `o${Math.min(i + 1, 5)}`;
  };
}

/** Everything the components need, computed once. */
export function derive(input: Timeline): View {
  const used = new Set<string>();
  const idMap = new Map<Entry, string>(input.messages.map((e) => [e, entryId(e, used)]));
  const idOf = (e: Entry) => idMap.get(e)!;
  const byId = new Map<string, Entry>();
  for (const [e, id] of idMap) byId.set(id, e);

  // Edited quote copies (issue #42) render inside the quoting message, not as rows. Their
  // replies are re-parented to the copy's own parent so the subtree stays attached.
  const hoisted = new Set<string>();
  for (const e of input.messages) {
    for (const ed of e.edits ?? []) if (ed.id && byId.has(ed.id)) hoisted.add(ed.id);
  }
  // Mutated in place: idOf keys by object identity, so a clone would lose its id.
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

  // Panel orgs are appended after message orgs, so a cc-only recipient can't repaint the transcript.
  const orgs: string[] = orgOrder(ordered.map((e) => e.org));
  for (const p of input.participants ?? []) if (p.org && !orgs.includes(p.org)) orgs.push(p.org);
  const slot = slotsFor(orgs);

  const avatarNames = Object.keys(input.avatars ?? {}).sort();
  const avatarClass = new Map(avatarNames.map((n, i) => [n, `p${i}`]));
  const avatarCss = avatarNames
    .map((n) => ({ n, v: avatarURL(input.avatars![n] ?? "") }))
    .filter(({ v }) => v !== null)
    .map(({ n, v }) => `.av.${avatarClass.get(n)}{background-image:url(${v})}`)
    .join("");

  const emails = new Map<string, string>();
  for (const e of ordered)
    if (e.sender && e.fromEmail && !emails.has(e.sender)) emails.set(e.sender, e.fromEmail);
  for (const p of spec.participants ?? []) if (p.email) emails.set(p.name, p.email);

  const firstRow = new Map(lay.chains.map((c) => [c.root, c.firstRow]));
  const laneOf = new Map(lay.chains.map((c) => [c.root, c.lane]));

  const rows: Row[] = ordered.map((entry) => {
    const id = idOf(entry);
    const chain = lay.chainOf.get(id)!;
    const lbl = z.label(entry);
    // The copy's markup keeps the pasted formatting; who and when come from the quoting host.
    const editEntry = (id: string | undefined): EditEntry | undefined => {
      const e = id ? byId.get(id) : undefined;
      if (!e) return undefined;
      return {
        html: e.body,
        who: e.sender ?? "",
        stamp: [e.date, e.time].filter(Boolean).join(" "),
      };
    };
    const edits = resolveEdits(entry.edits, editEntry, {
      who: entry.sender ?? "",
      time: entry.time ?? "",
    });
    return {
      entry,
      id,
      chain,
      row: lay.row.get(id)!,
      lane: laneOf.get(chain)!,
      isChainStart: lay.row.get(id) === firstRow.get(chain),
      orgSlot: slot(entry.org),
      avatarClass: entry.sender ? avatarClass.get(entry.sender) : undefined,
      edits,
      stamp: { date: entry.date, time: entry.time, tz: lbl.tz, zone: lbl.state },
    };
  });

  const title = (input.title ?? "Timeline").replace(/^#+/, (m) => (m ? "#" : ""));
  return {
    spec,
    rows,
    layout: lay,
    zones: z,
    orgs,
    orgSlot: slot,
    title,
    avatarCss,
    hashed: title.startsWith("#"),
    whoTitle: (name) => (emails.has(name) ? `${name} <${emails.get(name)}>` : name),
  };
}

export { initials };

/**
 * Security boundary: the value is emitted into a <style> url(), so only http(s) or
 * data: values with no url()-escaping characters pass. Null means use initials.
 */
export function avatarURL(v: string): string | null {
  const low = v.toLowerCase();
  const okScheme =
    low.startsWith("data:image/") || low.startsWith("http://") || low.startsWith("https://");
  if (!okScheme) return null;
  // eslint-disable-next-line no-control-regex -- reject control bytes in sender-provided URLs.
  return /[\s"'\\()<>`\u0000-\u001f]/.test(v) ? null : v;
}
