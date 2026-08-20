import type { Timeline, Entry } from "./spec";

/**
 * Accept legacy snake_case specs (as produced by the original Python build.py)
 * alongside the camelCase contract.
 *
 * This exists so specs collected before the schema was formalised still load.
 * It is deliberately a one-way translation at the boundary: everything past this
 * point sees only the contract, so there is exactly one shape to reason about.
 */
const TOP: Record<string, keyof Timeline> = {
  open_items: "openItems",
  open_items_title: "openItemsTitle",
  source_notes: "sourceNotes",
  run_label: "runLabel",
};

const ENTRY: Record<string, keyof Entry> = {
  from_email: "fromEmail",
  gmail_id: "gmailId",
  thread_id: "threadId",
};

/** Legacy notices used kind:"sys"; the contract calls them notes. */
function entryKind(raw: Record<string, unknown>): Entry["kind"] {
  const k = raw.kind;
  if (k === "sys" || k === "note") return "note";
  return "message";
}

function renameKeys<T>(
  raw: Record<string, unknown>,
  map: Record<string, keyof T>,
  drop: string[] = [],
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(raw)) {
    // internal render state was previously persisted with a leading underscore
    if (k.startsWith("_") || drop.includes(k)) continue;
    out[(map[k] as string) ?? k] = v;
  }
  return out;
}

export function normalise(input: unknown): Timeline {
  if (typeof input !== "object" || input === null) {
    throw new Error("spec must be an object");
  }
  const raw = input as Record<string, unknown>;
  const top = renameKeys<Timeline>(raw, TOP, ["messages"]);

  const messages = (raw.messages as Record<string, unknown>[] | undefined) ?? [];
  top.messages = messages.map((m) => {
    const e = renameKeys<Entry>(m, ENTRY, ["kind"]);
    e.kind = entryKind(m);
    if (Array.isArray(m.attachments)) {
      e.attachments = (m.attachments as Record<string, unknown>[]).map((a) =>
        renameKeys<NonNullable<Entry["attachments"]>[number]>(a, { gmail_id: "gmailId" } as never),
      );
    }
    return e;
  });

  top.specVersion = (raw.specVersion as number) ?? 1;
  return top as unknown as Timeline;
}
