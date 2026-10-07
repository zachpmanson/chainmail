import { describe, expect, it } from "vitest";
import { avatarURL, derive, orgOrder, slotsFor } from "../src/lib/timeline/derive";
import { load, msg, timeline } from "./fixtures";

describe("orgOrder and slotsFor", () => {
  it("orders orgs by first sighting", () => {
    expect(orgOrder(["Loom", undefined, "Acme", "Loom", ""])).toEqual(["Loom", "Acme"]);
  });

  it("caps slots at o5, which unknown orgs share", () => {
    const slot = slotsFor(["a", "b", "c", "d", "e", "f"]);
    expect(["a", "d", "e", "f", "zz", undefined].map(slot)).toEqual([
      "o1",
      "o4",
      "o5",
      "o5",
      "o5",
      "o5",
    ]);
  });
});

describe("avatarURL", () => {
  it("keeps data: images and http(s) URLs", () => {
    expect(avatarURL("data:image/png;base64,iVBORw0KGgo=")).toBe(
      "data:image/png;base64,iVBORw0KGgo=",
    );
    expect(avatarURL("https://faces.example/ada.png?size=48")).toBe(
      "https://faces.example/ada.png?size=48",
    );
  });

  it("refuses other schemes and anything that could escape url()", () => {
    for (const v of [
      "javascript:alert(1)",
      "data:text/html;base64,PHNjcmlwdD4=",
      "//evil.example/x.png",
      'https://e.example/x.png" onerror="x',
      "https://e.example/x.png)",
      "https://e.example/ x.png",
      "https://e.example/x\u0000.png",
    ])
      expect(avatarURL(v)).toBeNull();
  });
});

describe("derive", () => {
  const thread = () =>
    timeline(
      msg("a", { sender: "Ada", fromEmail: "ada@loom.example", org: "Loom", time: "09:00" }),
      msg("copy", { parent: "a", sender: "Ada", time: "09:00" }),
      msg("q", {
        parent: "a",
        sender: "Bo",
        org: "Acme",
        time: "10:00",
        edits: [{ id: "copy", base: "a", body: "a, edited" }],
      }),
      msg("r", { parent: "copy", sender: "Cy", time: "11:00" }),
    );

  it("hoists an edited copy out and re-parents its replies", () => {
    const v = derive(thread());
    expect(v.rows.map((r) => [r.id, r.entry.parent])).toEqual([
      ["a", undefined],
      ["q", "a"],
      ["r", "a"],
    ]);
  });

  it("attributes the edit to the quoter and the original to its sender", () => {
    const q = derive(thread()).rows.find((r) => r.id === "q")!;
    expect(q.edits).toHaveLength(1);
    expect(q.edits![0]).toMatchObject({
      base: "a",
      who: "Bo",
      time: "10:00",
      origWho: "Ada",
      origStamp: "Mon 2 Mar 2026 09:00",
    });
  });

  it("colours orgs and titles senders with their address", () => {
    const v = derive(thread());
    expect(v.orgs).toEqual(["Loom", "Acme"]);
    expect(v.rows.map((r) => r.orgSlot)).toEqual(["o1", "o2", "o5"]);
    expect(v.whoTitle("Ada")).toBe("Ada <ada@loom.example>");
    expect(v.whoTitle("Cy")).toBe("Cy");
  });

  it("collapses a run of leading hashes to one and flags it", () => {
    expect(derive({ ...thread(), title: "##daystrom" })).toMatchObject({
      title: "#daystrom",
      hashed: true,
    });
    expect(derive({ ...thread(), title: undefined as unknown as string })).toMatchObject({
      title: "Timeline",
      hashed: false,
    });
  });

  it("emits a CSS rule only for safe avatars", () => {
    const v = derive({
      ...thread(),
      avatars: { Ada: "https://faces.example/ada.png", Bo: "javascript:alert(1)" },
    });
    expect(v.avatarCss).toBe(".av.p0{background-image:url(https://faces.example/ada.png)}");
    expect(v.rows.map((r) => r.avatarClass)).toEqual(["p0", "p1", undefined]);
  });

  it("derives a row per synthetic entry", () => {
    expect(derive(load("synthetic")).rows).toHaveLength(58);
  });
});
