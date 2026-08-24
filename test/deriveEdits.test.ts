import { describe, expect, it } from "vitest";
import { derive, type RowEdit } from "../src/lib/derive";
import type { Timeline, Entry } from "../src/lib/spec";

/** The CSV worked example from issue #42: Charles 09:00, Jason 14:00 with an
 *  edited quote. The forwarded (edited) copy occupies a row; the renderer must
 *  hoist it into the host's bubble instead of floating it. */
const csv = (): Timeline => {
  const base: Entry = {
    id: "c-orig", date: "Fri 21 Aug 2026", time: "09:00", tz: "+1000",
    sender: "Charles", org: "ruralco",
    body: "<p>CSV layout: ... E: Amount Due</p>",
  };
  const edited: Entry = {
    id: "c-edit", date: "Fri 21 Aug 2026", time: "09:00", tz: "+1000",
    sender: "Charles", org: "ruralco", quoted: true, parent: base.id,
    body: "<p>CSV layout: ... E: Invoice Amount</p>",
  };
  const host: Entry = {
    id: "j-host", date: "Fri 21 Aug 2026", time: "14:00", tz: "+1000",
    sender: "Jason", org: "termina", parent: base.id,
    body: "<p>Actually one change — we track Invoice Amount.</p>",
    edits: [{
      id: edited.id, base: base.id, who: "Jason", time: "14:00",
      body: "CSV layout: ... E: Invoice Amount",
    }],
  };
  return { title: "#CSV layout", messages: [base, edited, host] };
};

describe("hoist for a quoter's edit (#42)", () => {
  it("keeps the edited copy out of the grid, base and host in", () => {
    const v = derive(csv());
    const ids = v.rows.map((r) => r.id);
    expect(ids).toContain("c-orig"); // the original renders once
    expect(ids).toContain("j-host"); // the reply renders
    expect(ids).not.toContain("c-edit"); // the edited copy does not float
  });

  it("resolves the host's edits into a diff-marked block", () => {
    const v = derive(csv());
    const host = v.rows.find((r) => r.id === "j-host")!;
    expect(host.edits).toBeDefined();
    const [ed] = host.edits ?? ([] as RowEdit[]);
    expect(ed?.base).toBe("c-orig");
    expect(ed?.who).toBe("Jason");
    expect(ed?.time).toBe("14:00");
    expect(ed?.html).toContain("edel"); // the change is struck/inserted
    expect(ed?.html).toContain("eins");
  });

  it("leaves a spec with no edits untouched", () => {
    const t: Timeline = {
      title: "T",
      messages: [{
        id: "a", date: "2026-08-21", time: "09:00", sender: "Ada", body: "<p>hi</p>",
      }],
    };
    const v = derive(t);
    expect(v.rows).toHaveLength(1);
    expect(v.rows[0]!.edits).toBeUndefined();
  });
});