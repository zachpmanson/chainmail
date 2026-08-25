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
    // the original message's sender and timestamp feed the “from y at <ts>” header
    expect(ed?.origWho).toBe("Charles");
    expect(ed?.origStamp).toBe("Fri 21 Aug 2026 09:00");
    // the added answer is highlighted as new to the original
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

  it("hoists the copy even when the host replied to it (the real chain)", () => {
    // The Termina x Ruralco CSV thread: Zach re-sends his own 14:40 message
    // edited at 16:41, and Jason's 20:04 reply re-quotes and carries that edit.
    // So in the spec the host's parent is the EDITED COPY, not the base — the
    // shape the CSV worked example did not cover. This is the #42 example Zach
    // pointed at, and it must not stay a floating node.
    const base: Entry = {
      id: "z-base", date: "Thu 20 Aug 2026", time: "14:40", tz: "AEST",
      sender: "Zach", org: "termina",
      body: "<p>I want to confirm Hewson Farms ... through your system?</p>",
    };
    const edited: Entry = {
      id: "z-1641", date: "Thu 20 Aug 2026", time: "16:41", tz: "AEST",
      sender: "Zach", org: "termina", quoted: true, parent: base.id,
      body: "<p>I want to confirm Hewson Farms ... through your system? - Yes, PDF copies.</p>",
    };
    const host: Entry = {
      id: "j-2004", date: "Thu 20 Aug 2026", time: "20:04", tz: "AEST",
      sender: "Jason", org: "ruralco", parent: edited.id, // host replier replied TO the copy
      body: "<p>Morning Charles ... I answered #3 in red below.</p>",
      edits: [{
        id: edited.id, base: base.id, who: "Jason", time: "20:04",
        body: "I want to confirm Hewson Farms ... through your system? - Yes, PDF copies.",
      }],
    };
    const v = derive({ title: "# CSV", messages: [base, edited, host] });
    const ids = v.rows.map((r) => r.id);
    expect(ids).toContain("z-base");
    expect(ids).toContain("j-2004");
    expect(ids).not.toContain("z-1641"); // hoisted: not a floating node
    // The host's reply has been re-parented onto the base, not a dangling copy id:
    // its chain root is the base, which is what keeps it on the base's lane.
    const hostRow = v.rows.find((r) => r.id === "j-2004")!;
    expect(hostRow.chain).toBe("z-base");
    // And the edit reaches the bubble, diffed against the base.
    const [ed] = hostRow.edits ?? ([] as RowEdit[]);
    expect(ed?.base).toBe("z-base");
    expect(ed?.who).toBe("Jason");
    expect(ed?.html).toContain("eins");
  });
});