import { describe, expect, it } from "vitest";
import { gmailIdOf, msgCount, provenance, sourceLine } from "../src/lib/message/sources";
import { corpus } from "./fixtures";

describe("provenance", () => {
  it("parses the generated unspooled line into ids", () => {
    expect(provenance(" unspooled from msg a1b2, quote:9f3c ")).toEqual({
      kind: "ids",
      prefix: "unspooled from ",
      ids: [{ text: "msg a1b2", gmailId: "a1b2" }, { text: "quote:9f3c" }],
    });
  });

  it("reads a bare id list with no prefix", () => {
    expect(provenance("slack:C01:1712.0001")).toEqual({
      kind: "ids",
      prefix: "",
      ids: [{ text: "slack:C01:1712.0001" }],
    });
  });

  it("renders anything else verbatim as prose", () => {
    expect(provenance("forwarded by Ada, msg a1b2")).toEqual({
      kind: "prose",
      text: "forwarded by Ada, msg a1b2",
    });
    expect(provenance("unspooled from ")).toEqual({ kind: "prose", text: "unspooled from" });
  });
});

describe("msgCount", () => {
  it("pluralises", () => {
    expect([0, 1, 2].map(msgCount)).toEqual(["0 msgs", "1 msg", "2 msgs"]);
  });
});

describe("gmailIdOf", () => {
  it("reads a Gmail permalink's message id, and nothing else", () => {
    expect(
      gmailIdOf(corpus("x", { permalink: "https://mail.google.com/mail/#all/19fee08b" })),
    ).toBe("19fee08b");
    expect(
      gmailIdOf(corpus("x", { permalink: "https://slack.example/archives/C01/p1" })),
    ).toBeUndefined();
    expect(gmailIdOf(corpus("x"))).toBeUndefined();
  });
});

describe("sourceLine", () => {
  const name = (id: string) => `msg ${id.slice(6, 8)}`;

  it("names a direct message by its mailbox id, else its ext id", () => {
    const e = corpus("mail:<c0ffee@loom>", { permalink: "https://mail.google.com/mail/#all/19fe" });
    expect(sourceLine(e, name)).toBe("msg 19fe");
    expect(sourceLine(corpus("slack:C01:1712.0001"), name)).toBe("slack:C01:1712.0001");
  });

  it("names each quoting host once, skipping direct sightings", () => {
    const e = corpus("quote:ab", {
      quoted: true,
      sightings: [
        { kind: "direct", seenIn: "mail:<zz>" },
        { kind: "quoted", seenIn: "mail:<h1>" },
        { kind: "forwarded", seenIn: "mail:<h1>" },
        { kind: "quoted", seenIn: "mail:<h2>" },
      ],
    });
    const line = sourceLine(e, name);
    expect(line).toBe("unspooled from msg h1, msg h2");
    expect(provenance(line).kind).toBe("ids");
  });

  it("says quoted text when no host is known", () => {
    expect(
      sourceLine(corpus("quote:ab", { quoted: true, sightings: [{ kind: "quoted" }] }), name),
    ).toBe("unspooled from quoted text");
  });
});
