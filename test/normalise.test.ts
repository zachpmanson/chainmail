import { describe, expect, it } from "vitest";
import { normalise } from "../src/lib/timeline/normalise";

describe("normalise", () => {
  it("translates legacy snake_case keys and kind:sys", () => {
    expect(
      normalise({
        title: "x",
        open_items: ["a"],
        run_label: "r1",
        messages: [
          { kind: "sys", date: "Mon 17 Aug 2026", body: "", label: "call" },
          {
            date: "Tue 18 Aug 2026",
            body: "",
            from_email: "a@b.c",
            gmail_id: "g1",
            attachments: [{ name: "f.pdf", gmail_id: "g2" }],
          },
        ],
      }),
    ).toEqual({
      title: "x",
      openItems: ["a"],
      runLabel: "r1",
      specVersion: 1,
      messages: [
        { kind: "note", date: "Mon 17 Aug 2026", body: "", label: "call" },
        {
          kind: "message",
          date: "Tue 18 Aug 2026",
          body: "",
          fromEmail: "a@b.c",
          gmailId: "g1",
          attachments: [{ name: "f.pdf", gmailId: "g2" }],
        },
      ],
    });
  });

  it("drops underscore-prefixed render state", () => {
    const t = normalise({ title: "x", _cache: 1, messages: [{ date: "d", body: "", _new: true }] });
    expect(Object.keys(t)).not.toContain("_cache");
    expect(Object.keys(t.messages[0]!)).toEqual(["date", "body", "kind"]);
  });

  it("reads a null messages list as empty and keeps a stated version", () => {
    expect(normalise({ title: "x", messages: null, specVersion: 3 })).toEqual({
      title: "x",
      messages: [],
      specVersion: 3,
    });
  });

  it("refuses a spec that is not an object", () => {
    expect(() => normalise([])).toThrow("spec must be an object");
    expect(() => normalise(null)).toThrow("spec must be an object");
  });

  it("names the field that is wrong", () => {
    expect(() => normalise({ messages: [] })).toThrow("spec: title must be a string");
    expect(() => normalise({ title: "x", messages: {} })).toThrow(
      "spec: messages must be an array",
    );
    expect(() => normalise({ title: "x", messages: [{ body: "" }] })).toThrow(
      "spec: messages[0].date must be a string",
    );
    expect(() =>
      normalise({ title: "x", messages: [{ date: "d", body: "", attachments: [1] }] }),
    ).toThrow("spec: messages[0].attachments[0] must be an object");
  });
});
