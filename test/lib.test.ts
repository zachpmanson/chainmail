import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { normalise } from "../src/lib/normalise";
import { entryId } from "../src/lib/anchors";
import { order, zones, tzMinutes } from "../src/lib/chronological";
import { layout, isMeta } from "../src/lib/lanes";
import { avatarURL } from "../src/lib/derive";
import type { Entry } from "../src/lib/spec";
import { gmailIdOf, provenance, sourceLine } from "../src/lib/sources";
import type { CorpusEntry } from "../src/lib/api";
import { MAX_COLS, MAX_ROWS, couldBeTable, parseDelimited, readTable } from "../src/lib/tables";

const load = (f: string) => normalise(JSON.parse(readFileSync(`fixtures/${f}.json`, "utf8")));
const withIds = (es: Entry[]) => {
  const used = new Set<string>();
  const m = new Map(es.map((e) => [e, entryId(e, used)]));
  return (e: Entry) => m.get(e)!;
};

describe("timezones", () => {
  it("parses labels and numeric offsets", () => {
    expect(tzMinutes("NZST")).toBe(720);
    expect(tzMinutes("AEST")).toBe(600);
    expect(tzMinutes("+0530")).toBe(330);
    expect(tzMinutes("nonsense")).toBeNull();
  });

  it("orders by absolute time, not the displayed clock", () => {
    // 09:51 NZST is 21:51 UTC; the 09:20 AEST reply the same morning is 23:20 UTC
    const t = load("synthetic");
    const z = zones(t.messages);
    const nz = t.messages.find((e) => e.time === "09:51" && e.tz === "NZST")!;
    const au = t.messages.find((e) => e.time === "09:20" && e.tz === "AEST")!;
    expect(nz.time! > au.time!).toBe(true); // the clock disagrees...
    expect(z.absolute(nz)).toBeLessThan(z.absolute(au)); // ...absolute time does not
    const idOf = withIds(t.messages);
    const seq = order(t.messages, idOf).map((e) => idOf(e));
    expect(seq.indexOf(idOf(nz))).toBeLessThan(seq.indexOf(idOf(au)));
  });

  it("shows no zone at all where the spec states none", () => {
    const t = load("minimal");
    const z = zones(t.messages);
    const e = t.messages[0]!;
    expect(e.tz).toBeUndefined();
    expect(z.label(e)).toEqual({ tz: undefined, state: "unknown" });
  });

  it("calls every unstated zone unknown, notes included, and invents none", () => {
    const t = load("synthetic");
    const z = zones(t.messages);
    const msgs = t.messages.filter((e) => e.kind !== "note");
    expect(msgs.filter((e) => e.tz)).toHaveLength(37);
    expect(msgs.filter((e) => !e.tz)).toHaveLength(18);
    expect(t.messages.filter((e) => !e.tz)).toHaveLength(21);
    const unstated = t.messages.filter((e) => !e.tz);
    expect(unstated.every((e) => z.label(e).state === "unknown")).toBe(true);
    expect(unstated.every((e) => z.label(e).tz === undefined)).toBe(true);
  });

  it("distinguishes an inferred zone from a stated one", () => {
    const stated = { date: "Mon 2 Mar 2026", body: "", tz: "AEST" } as const;
    const guessed = { date: "Mon 2 Mar 2026", body: "", tz: "+1000", tzSource: "inferred" } as const;
    const z = zones([stated, guessed]);
    expect(z.label(stated).state).toBe("stated");
    expect(z.label(guessed).state).toBe("inferred");
    // the same clock either way: a source note is the place for the caveat, not
    // a different absolute time
    expect(z.absolute(stated)).toBe(z.absolute(guessed));
  });
});

describe("ordering", () => {
  it("never places a reply above its parent", () => {
    const t = load("synthetic");
    const idOf = withIds(t.messages);
    const seq = order(t.messages, idOf);
    const pos = new Map(seq.map((e, i) => [idOf(e), i]));
    expect(seq.filter((e) => e.parent && pos.get(e.parent)! >= pos.get(idOf(e))!)).toEqual([]);
  });

  it("keeps every entry", () => {
    const t = load("synthetic");
    expect(order(t.messages, withIds(t.messages))).toHaveLength(58);
  });

  it("does not drop entries when a parent cycle exists", () => {
    const cyclic: Entry[] = [
      { kind: "message", id: "x", date: "Tue 1 Jul 2025", body: "", parent: "y" },
      { kind: "message", id: "y", date: "Tue 1 Jul 2025", body: "", parent: "x" },
    ];
    expect(order(cyclic, withIds(cyclic))).toHaveLength(2);
  });
});

describe("lanes", () => {
  it("recycles a lane once a chain has ended: 7 chains share 4", () => {
    const t = load("synthetic");
    const idOf = withIds(t.messages);
    const l = layout(order(t.messages, idOf), idOf);
    expect(l.chains).toHaveLength(7);
    expect(l.laneCount).toBe(4);
    const perLane = new Map<number, number>();
    for (const c of l.chains) perLane.set(c.lane, (perLane.get(c.lane) ?? 0) + 1);
    expect([...perLane.values()].some((n) => n > 1)).toBe(true);
  });

  it("never overlaps two chains in one lane", () => {
    const t = load("synthetic");
    const idOf = withIds(t.messages);
    const l = layout(order(t.messages, idOf), idOf);
    for (const a of l.chains) {
      for (const b of l.chains) {
        if (a === b || a.lane !== b.lane) continue;
        expect(a.lastRow < b.firstRow || b.lastRow < a.firstRow).toBe(true);
      }
    }
  });

  it("classifies meeting-only chains as meta and leaves mixed chains alone", () => {
    const t = load("synthetic");
    const idOf = withIds(t.messages);
    const l = layout(order(t.messages, idOf), idOf);
    expect(l.chains.filter((c) => c.meta).map((c) => c.entries.length).sort()).toEqual([1, 2]);
    // the longest chain contains a meeting notice but also real correspondence
    expect(l.chains.find((c) => c.entries.length === 27)!.meta).toBe(false);
  });

  it("treats an invitation body as meta by its leading token only", () => {
    const mk = (body: string) => ({ kind: "message", date: "x", body }) as Entry;
    expect(isMeta(mk("<p>Invitation: standup</p>"))).toBe(true);
    expect(isMeta(mk("<p>Notes: what we agreed</p>"))).toBe(true);
    expect(isMeta(mk("<p>see the invitation I sent</p>"))).toBe(false);
    expect(isMeta({ ...mk("<p>Invitation: x</p>"), meta: false })).toBe(false);
  });
});

describe("anchors", () => {
  it("derives ids from content and de-duplicates", () => {
    const used = new Set<string>();
    const e = { kind: "message", date: "Thu 16 Jul 2026", time: "11:35", sender: "Jean-Luc Picard", body: "" } as Entry;
    expect(entryId(e, used)).toBe("m-20260716-1135-jlp");
    expect(entryId(e, used)).toBe("m-20260716-1135-jlp-2");
  });

  it("names a note by its date", () => {
    const used = new Set<string>();
    expect(entryId({ kind: "note", date: "Mon 17 Aug 2026", body: "" } as Entry, used)).toBe("m-20260817-note");
  });
});

describe("legacy specs", () => {
  it("accepts snake_case keys and kind:sys", () => {
    const t = normalise({
      title: "x",
      open_items: ["a"],
      messages: [{ kind: "sys", date: "Mon 17 Aug 2026", body: "", label: "call" },
                 { date: "Tue 18 Aug 2026", body: "", sender: "A B", from_email: "a@b.c", gmail_id: "g1" }],
    });
    expect(t.openItems).toEqual(["a"]);
    expect(t.messages[0]!.kind).toBe("note");
    expect(t.messages[1]!.fromEmail).toBe("a@b.c");
    expect(t.messages[1]!.gmailId).toBe("g1");
  });

  it("drops internal render state that older specs persisted", () => {
    const t = normalise({ title: "x", messages: [{ date: "d", body: "", _new: true, _id: "z" }] });
    expect(Object.keys(t.messages[0]!)).not.toContain("_new");
  });
});

describe("interlinking", () => {
  it("every xref in a body points at an entry that exists", () => {
    const t = load("synthetic");
    const used = new Set<string>();
    const ids = new Set(t.messages.map((e) => entryId(e, used)));
    const hrefs = [...JSON.stringify(t).matchAll(/href=\\?['"]#(m-[^'"\\]+)/g)].map((m) => m[1]!);
    expect(hrefs).toHaveLength(37); // editorial cross-links plus open-item links
    expect(hrefs.filter((h) => !ids.has(h))).toEqual([]);
  });

  it("every parent points at an entry that exists", () => {
    const t = load("synthetic");
    const used = new Set<string>();
    const ids = new Set(t.messages.map((e) => entryId(e, used)));
    const parents = t.messages.map((e) => e.parent).filter(Boolean) as string[];
    expect(parents).toHaveLength(51);
    expect(parents.filter((p) => !ids.has(p))).toEqual([]);
  });
});

describe("chain filtering", () => {
  it("re-derives lanes after a chain is excluded", () => {
    const t = load("synthetic");
    const idOf0 = withIds(t.messages);
    const full = layout(order(t.messages, idOf0), idOf0);
    expect(full.chains).toHaveLength(7);
    expect(full.laneCount).toBe(4);

    // drop the largest chain and re-derive from scratch
    const biggest = full.chains.reduce((a, b) => (a.entries.length > b.entries.length ? a : b));
    const kept = t.messages.filter((e) => full.chainOf.get(idOf0(e)) !== biggest.root);
    expect(kept).toHaveLength(t.messages.length - biggest.entries.length);

    const idOf1 = withIds(kept);
    const after = layout(order(kept, idOf1), idOf1);
    expect(after.chains).toHaveLength(6);
    expect(after.laneCount).toBeLessThanOrEqual(full.laneCount);
    // rows must still be a contiguous run, not the old numbering with gaps
    const rows = [...after.row.values()].sort((a, b) => a - b);
    expect(rows).toEqual(rows.map((_, i) => i + 2));
  });

  it("keeps every remaining entry's parent inside the kept set", () => {
    const t = load("synthetic");
    const idOf0 = withIds(t.messages);
    const full = layout(order(t.messages, idOf0), idOf0);
    for (const drop of full.chains) {
      const kept = t.messages.filter((e) => full.chainOf.get(idOf0(e)) !== drop.root);
      const ids = new Set(kept.map((e) => idOf0(e)));
      // excluding whole chains can never orphan a parent
      expect(kept.filter((e) => e.parent && !ids.has(e.parent))).toEqual([]);
    }
  });
});

describe("avatarURL — the avatar value's boundary before it is emitted into url()", () => {
  it("keeps data: images and http(s) URLs", () => {
    expect(avatarURL("data:image/png;base64,iVBORw0KGgo=")).toBe("data:image/png;base64,iVBORw0KGgo=");
    expect(avatarURL("https://faces.example/ada.png?size=48")).toBe("https://faces.example/ada.png?size=48");
    expect(avatarURL("http://faces.example/bo.jpg")).toBe("http://faces.example/bo.jpg");
  });

  it("refuses schemes a page should never load", () => {
    expect(avatarURL("javascript:alert(1)")).toBeNull();
    expect(avatarURL("data:text/html;base64,PHNjcmlwdD4=")).toBeNull();
    expect(avatarURL("file:///etc/passwd")).toBeNull();
    expect(avatarURL("vbscript:msgbox(1)")).toBeNull();
    expect(avatarURL("/relative/pic.png")).toBeNull();
    expect(avatarURL("//evil.example/x.png")).toBeNull();
  });

  it("refuses bytes that could end or escape the url() token", () => {
    expect(avatarURL('https://e.example/x.png" onerror="alert(1)')).toBeNull();
    expect(avatarURL("https://e.example/x.png)")).toBeNull();
    expect(avatarURL("https://e.example/x.png\\")).toBeNull();
    expect(avatarURL("https://e.example/ x.png")).toBeNull();
    expect(avatarURL("https://e.example/x.png\n}")).toBeNull();
    expect(avatarURL("https://e.example/<script>")).toBeNull();
  });
});

describe("the provenance line a chain pane builds", () => {
  const entry = (e: Partial<CorpusEntry>): CorpusEntry =>
    ({ extId: "mail:<x@y>", source: "mail", quoted: false, ts: "2026-03-02T09:00:00Z", ...e }) as CorpusEntry;

  it("names a message the mailbox holds by its mailbox id", () => {
    const e = entry({
      extId: "mail:<c0ffee@loomworks.example>",
      permalink: "https://mail.google.com/mail/u/0/#all/19fee08b9d28e28b",
      sightings: [{ kind: "direct" }],
    });
    // The same words the generator writes, because provenance() is what reads it
    // back: a line in any other shape would render as prose.
    expect(sourceLine(e, (id) => id)).toBe("msg 19fee08b9d28e28b");
    expect(provenance(sourceLine(e, (id) => id))).toEqual({
      kind: "ids",
      prefix: "",
      ids: [{ text: "msg 19fee08b9d28e28b", gmailId: "19fee08b9d28e28b" }],
    });
  });

  it("falls back to the corpus ext id where no mailbox id is known", () => {
    // A mailbox this build cannot name, or a source that is not a mailbox at
    // all: the id it is held under is still the id a reader can quote, so it is
    // shown rather than dropped.
    const e = entry({ extId: "slack:<C0123>:1712345678.000100", sightings: [{ kind: "direct" }] });
    expect(sourceLine(e, (id) => id)).toBe("slack:<C0123>:1712345678.000100");
    expect(gmailIdOf(e)).toBeUndefined();
    // And a permalink that is not Gmail's is not read as though it were.
    expect(
      gmailIdOf(entry({ permalink: "https://example.slack.com/archives/C0123/p1712345678000100" })),
    ).toBeUndefined();
  });

  it("names the hosts a recovered message was unspooled from, once each", () => {
    const seen = entry({
      extId: "quote:a829678324a518162a2d85db51897b7e",
      quoted: true,
      sightings: [
        { kind: "quoted", seenIn: "mail:<host-1@loomworks.example>" },
        // A host that both quoted and forwarded arrives twice: one message to
        // open, so the line may not count it twice.
        { kind: "quoted", seenIn: "mail:<host-1@loomworks.example>", detail: "depth 2" },
        { kind: "quoted", seenIn: "mail:<host-2@loomworks.example>" },
      ],
    });
    const named = sourceLine(seen, (id) => "msg " + (id === "mail:<host-1@loomworks.example>" ? "a1b2" : "c3d4"));
    expect(named).toBe("unspooled from msg a1b2, msg c3d4");
    expect(provenance(named).kind).toBe("ids");
  });

  it("says a recovery has no host at all rather than naming nobody", () => {
    // A quote whose sighting names no host — the generator's own last resort,
    // for the same reason: the text came from somewhere and the corpus no longer
    // knows where.
    const e = entry({ extId: "quote:deadbeef", quoted: true, sightings: [{ kind: "quoted" }] });
    expect(sourceLine(e, (id) => id)).toBe("unspooled from quoted text");
  });
});

describe("reading a delimited file as a table", () => {
  it("splits on a delimiter, and not on one inside quotes", () => {
    // The whole reason this is not a `split(",")`: a quoted field is allowed to
    // contain the delimiter, and the quote that would have broken it.
    const rows = parseDelimited('name,amount\n"Okoye, Ada","1,204.00"\n"O""Brien",2\n', ",");
    expect(rows).toEqual([
      ["name", "amount"],
      ["Okoye, Ada", "1,204.00"],
      ['O"Brien', "2"],
    ]);
  });

  it("keeps a newline that is inside a field, and drops the one that ends it", () => {
    // A cell holding an address is the common case, and a parser that splits on
    // "\n" first mangles the row it lands in.
    const rows = parseDelimited('a,b\n"line one\nline two",2\n', ",");
    expect(rows).toEqual([
      ["a", "b"],
      ["line one\nline two", "2"],
    ]);
  });

  it("reads CRLF, a trailing delimiter and a missing final newline", () => {
    expect(parseDelimited("a,b\r\n1,2\r\n", ",")).toEqual([["a", "b"], ["1", "2"]]);
    expect(parseDelimited("a,b,\n", ",")).toEqual([["a", "b", ""]]);
    expect(parseDelimited("a,b", ",")).toEqual([["a", "b"]]);
  });

  it("leaves a stray quote as the character it is", () => {
    // Only a quote at the start of a field opens one. 6" and O"Brien are a
    // measurement and a name, and either would otherwise swallow the file.
    expect(parseDelimited('a,b\n6" pipe,O"Brien\n', ",")).toEqual([
      ["a", "b"],
      ['6" pipe', 'O"Brien'],
    ]);
  });

  it("asks the name and the type whether a file could be one at all", () => {
    expect(couldBeTable("readings.csv", "")).toBe(true);
    expect(couldBeTable("ledger.TSV", "")).toBe(true);
    expect(couldBeTable("export.tab", "")).toBe(true);
    expect(couldBeTable("data", "text/csv")).toBe(true);
    expect(couldBeTable("data", "text/tab-separated-values; charset=utf-8")).toBe(true);
    // Prose with commas in it is prose: only an announced table is parsed as one.
    expect(couldBeTable("slurp.log", "text/plain")).toBe(false);
    expect(couldBeTable("notes.txt", "")).toBe(false);
  });

  it("reads a comma file into a header and rows", () => {
    const t = readTable("shed,readings\nNova,41.2\nOrion,38.9\n", "readings.csv", "text/csv")!;
    expect(t.header).toEqual(["shed", "readings"]);
    expect(t.rows).toEqual([["Nova", "41.2"], ["Orion", "38.9"]]);
    expect(t.rowCount).toBe(2);
    expect(t.colCount).toBe(2);
    expect(t.numeric).toEqual([false, true]);
  });

  it("finds the delimiter the file actually uses", () => {
    // A European spreadsheet writes a semicolon, and a mainframe writes a pipe;
    // neither is called a .csv for the delimiter's sake.
    expect(readTable("a;b\n1;2\n", "export.csv", "")!.header).toEqual(["a", "b"]);
    expect(readTable("a|b\n1|2\n", "export.csv", "")!.header).toEqual(["a", "b"]);
    expect(readTable("a\tb\n1\t2\n", "ledger.tsv", "")!.header).toEqual(["a", "b"]);
    // And a file that only says so in its served type.
    expect(readTable("a\tb\n1\t2\n", "ledger", "text/tab-separated-values")!.colCount).toBe(2);
  });

  it("refuses a file that is not a table", () => {
    // One column is a list, and a list is text — which is what the window falls
    // back to. This is the guard that keeps prose out of a spreadsheet.
    expect(readTable("Dear Ada,\n\nThanks, and regards,\nBen\n", "letter.csv", "text/plain")).toBeNull();
    expect(readTable("", "readings.csv", "")).toBeNull();
    expect(readTable("a,b", "readings.csv", "")).toBeNull(); // a header with nothing under it
    expect(readTable("a,b\n1,2\n", "slurp.log", "")).toBeNull();
  });

  it("pads a ragged row rather than dropping it", () => {
    const t = readTable("a,b,c\n1,2\n3,4,5\n", "readings.csv", "")!;
    expect(t.rows).toEqual([["1", "2", ""], ["3", "4", "5"]]);
  });

  it("caps what it shows and counts what it saw", () => {
    const header = Array.from({ length: 50 }, (_, i) => `c${i}`).join(",");
    const body = [header, ...Array.from({ length: 700 }, () => header)].join("\n");
    const t = readTable(body, "wide.csv", "")!;
    expect(t.header.length).toBe(MAX_COLS);
    expect(t.rows.length).toBe(MAX_ROWS);
    expect(t.rowCount).toBe(700);
    expect(t.colCount).toBe(50);
  });
});
