import { describe, expect, it } from "vitest";
import {
  addressesOf,
  receiptNames,
  senderTitle,
  withAddress,
  wordsOf,
} from "../src/lib/message/who";
import { corpus, MON_2_MAR } from "./fixtures";

describe("addressesOf", () => {
  it("keeps email identities only, once each", () => {
    expect(
      addressesOf([
        "email:ada@loom.example",
        "slack:U01",
        "name:Ada",
        "email:",
        "email:ada@loom.example",
      ]),
    ).toEqual(["ada@loom.example"]);
    expect(addressesOf(undefined)).toEqual([]);
  });
});

describe("withAddress", () => {
  it("appends addresses when there are any", () => {
    expect(withAddress("Ada", ["a@x", "b@y"])).toBe("Ada <a@x, b@y>");
    expect(withAddress("Ada", [])).toBe("Ada");
  });
});

describe("receiptNames", () => {
  it("splits a to: line and strips the cc/bcc marker", () => {
    expect(receiptNames("Bo Halvorsen, cc Cy Okafor, , BCC Di")).toEqual([
      { text: "Bo Halvorsen", name: "Bo Halvorsen" },
      { text: "cc Cy Okafor", name: "Cy Okafor" },
      { text: "BCC Di", name: "Di" },
    ]);
  });
});

describe("senderTitle", () => {
  it("names a sender with their address", () => {
    expect(senderTitle(corpus("x", { author: "Ada", fromEmail: "ada@loom.example" }))).toBe(
      "Ada <ada@loom.example>",
    );
    expect(senderTitle(corpus("x", { fromEmail: "ada@loom.example" }))).toBe("ada@loom.example");
  });

  it("names the quoter for a message with no From of its own", () => {
    expect(senderTitle(corpus("x", { author: "Ada", fromQuotedBy: "Bo <bo@x>" }))).toBe(
      "Ada (address unknown; quoted by Bo <bo@x>)",
    );
    expect(senderTitle(corpus("x", { fromQuotedBy: "Bo <bo@x>" }))).toBe(
      "address unknown; quoted by Bo <bo@x>",
    );
    expect(senderTitle(corpus("x", { author: "Ada" }))).toBe("Ada");
  });
});

describe("wordsOf", () => {
  it("gives who, title and the bubble's clock", () => {
    expect(
      wordsOf(
        corpus("x", {
          author: "Ada",
          fromEmail: "ada@loom.example",
          ts: "2026-03-01T22:15:00Z",
          tz: "AEDT",
          tzOffsetMinutes: 660,
        }),
      ),
    ).toEqual({ who: "Ada", whoTitle: "Ada <ada@loom.example>", when: `${MON_2_MAR} 09:15` });
  });
});
