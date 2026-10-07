import type { Timeline, Entry } from "./spec";

/** Legacy snake_case specs (from the old Python build.py) are translated to the contract here. */
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

/** A plain object, as JSON parses one: not null, and not an array. */
function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Checked after translation; a dropped spec is untrusted, so refuse it naming the field. */
function check(t: Record<string, unknown>): void {
  if (typeof t.title !== "string") throw new Error("spec: title must be a string");
  (t.messages as Record<string, unknown>[]).forEach((e, i) => {
    if (typeof e.date !== "string") throw new Error(`spec: messages[${i}].date must be a string`);
    if (typeof e.body !== "string") throw new Error(`spec: messages[${i}].body must be a string`);
    if (e.attachments !== undefined && !Array.isArray(e.attachments)) {
      throw new Error(`spec: messages[${i}].attachments must be an array`);
    }
  });
}

export function normalise(input: unknown): Timeline {
  if (!isRecord(input)) {
    throw new Error("spec must be an object");
  }
  const raw = input;
  const top = renameKeys<Timeline>(raw, TOP, ["messages"]);

  // A Go nil slice is written as null.
  const messages = raw.messages ?? [];
  if (!Array.isArray(messages)) throw new Error("spec: messages must be an array");
  top.messages = messages.map((m: unknown, i) => {
    if (!isRecord(m)) throw new Error(`spec: messages[${i}] must be an object`);
    const e = renameKeys<Entry>(m, ENTRY, ["kind"]);
    e.kind = entryKind(m);
    if (Array.isArray(m.attachments)) {
      e.attachments = m.attachments.map((a: unknown, j) => {
        if (!isRecord(a))
          throw new Error(`spec: messages[${i}].attachments[${j}] must be an object`);
        return renameKeys<NonNullable<Entry["attachments"]>[number]>(a, {
          gmail_id: "gmailId",
        } as never);
      });
    }
    return e;
  });

  top.specVersion = (raw.specVersion as number) ?? 1;
  check(top);
  return top as unknown as Timeline;
}
