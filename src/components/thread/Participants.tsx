import type { CorpusEntry } from "../../lib/api/api";
import type { Timeline as Spec } from "../../lib/timeline/spec";

/** A participant as the panel needs it, however the caller supplied them. */
export type Person = NonNullable<Spec["participants"]>[number];

/** Each person's role is the strongest they hold anywhere in the thread; their org is
 *  taken from mail they sent, so pure readers get the unknown slot. */
export function castOfEntries(entries: CorpusEntry[]): Person[] {
  const out: Person[] = [];
  const at = new Map<number, number>();
  const orgs = new Map<number, string>();
  for (const e of entries) {
    for (const p of e.participants ?? []) {
      if (p.role === "from" && e.org && !orgs.has(p.personId)) orgs.set(p.personId, e.org);
    }
  }
  for (const e of entries) {
    for (const p of e.participants ?? []) {
      const i = at.get(p.personId);
      if (i === undefined) {
        at.set(p.personId, out.length);
        out.push({
          name: p.name,
          role: p.role,
          org: orgs.get(p.personId),
          // A thread read records no recipient addresses, only the sender's.
          email: p.role === "from" ? e.fromEmail : undefined,
        });
        continue;
      }
      const held = out[i]!;
      const role =
        RANK[p.role]! > (held.role === undefined ? 0 : RANK[held.role]!) ? p.role : held.role;
      const email = held.email ?? (p.role === "from" ? e.fromEmail : undefined);
      if (role !== held.role || email !== held.email) out[i] = { ...held, role, email };
    }
  }
  return out;
}

const RANK: Record<string, number> = { from: 3, to: 2, cc: 1 };
