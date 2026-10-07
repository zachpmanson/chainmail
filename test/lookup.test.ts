import { describe, expect, it } from "vitest";
import { hoistEdits } from "../src/lib/thread/hoist";
import { anchor, threadLookup } from "../src/lib/thread/lookup";
import { stampOf } from "../src/lib/ui/stamp";
import { corpus, MON_2_MAR } from "./fixtures";

const entries = [
  corpus("a", {
    author: "Ada",
    fromEmail: "ada@loom.example",
    org: "Loom",
    html: "<p>Due Thursday</p>",
    permalink: "https://mail.google.com/mail/#all/g-a",
    participants: [{ personId: 1, name: "Bo", role: "to" }],
  }),
  corpus("copy", { parent: "a", author: "Ada", html: "<p>Due Friday</p>" }),
  corpus("q", {
    parent: "a",
    author: "Bo",
    ts: "2026-03-02T10:00:00Z",
    edits: [{ id: "copy", base: "a", body: "Due Friday" }],
  }),
  corpus("r", { parent: "copy", author: "Cy" }),
];
const { shown, parentOf } = hoistEdits(entries);
const look = threadLookup(entries, shown, parentOf, new Map([[1, ["bo@acme.example"]]]));
const byId = (id: string) => entries.find((e) => e.extId === id)!;

describe("threadLookup", () => {
  it("anchors drawn entries by position", () => {
    expect(anchor(3)).toBe("entry-3");
    expect(["a", "q", "r"].map(look.anchorOf)).toEqual(["entry-0", "entry-1", "entry-2"]);
    expect([...look.anchorByGmail]).toEqual([["g-a", "entry-0"]]);
  });

  it("titles a sender, preferring the identity graph's addresses", () => {
    expect(look.titleOf("Ada")).toBe("Ada <ada@loom.example>");
    expect(look.titleOf("Bo")).toBe("Bo <bo@acme.example>");
    expect(look.titleOf("Zed")).toBe("Zed");
    expect(look.slot("Loom")).toBe("o1");
  });

  it("names a mail by its Gmail id, else its ext id", () => {
    expect(["a", "q", "copy"].map(look.mailName)).toEqual(["msg g-a", "q", "copy"]);
  });

  it("links a reply to its drawn parent, through a hoisted copy", () => {
    expect(look.replyOf(byId("r"))).toEqual({
      anchor: "entry-0",
      who: "Ada",
      whoTitle: "Ada <ada@loom.example>",
      when: `${MON_2_MAR} 09:15`,
    });
    expect(look.replyOf(byId("a"))).toBeNull();
  });

  it("resolves edits against hoisted copies and anchors the base", () => {
    const q = byId("q");
    const [ed] = look.editsOf(q, stampOf(q))!;
    expect(ed).toMatchObject({
      base: "entry-0",
      who: "Bo",
      time: "10:00",
      origWho: "Ada",
      origStamp: `${MON_2_MAR} 09:15`,
    });
    expect(ed!.html.replace(/ class="[^"]*"/g, "")).toBe("<p>Due <b>Friday</b></p>");
    expect(look.editsOf(byId("a"), stampOf(byId("a")))).toBeUndefined();
  });
});
