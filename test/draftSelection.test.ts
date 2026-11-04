import { describe, expect, it } from "vitest";
import {
  draftDestination,
  draftId,
  draftIdentity,
  validatedDraft,
  type SavedDraft,
} from "../src/lib/ui/draftSelection";

const compose: SavedDraft = {
  accountId: "A",
  id: "00012345678901234567890",
  kind: "compose",
  resolution: "compose",
  threadId: "not-a-root",
};
describe("explicit draft navigation", () => {
  it("preserves opaque IDs and rejects missing/repeated owners", () => {
    expect(draftId(compose.id)).toBe(compose.id);
    expect(draftId(" A ")).toBe(" A ");
    for (const value of [undefined, "", "  ", 123, ["A", "B"]])
      expect(draftId(value)).toBeUndefined();
    expect(draftIdentity("A", "D")).not.toBe(draftIdentity("B", "D"));
  });
  it("uses authoritative classification, never Gmail threadId", () => {
    expect(draftDestination(compose)).toBe("compose");
    expect(
      draftDestination({ ...compose, kind: "unresolved", resolution: "lookup-unavailable" }),
    ).toBeUndefined();
    expect(draftDestination({ ...compose, kind: "reply", resolution: "resolved" })).toBeUndefined();
    expect(
      draftDestination({
        ...compose,
        kind: "reply",
        resolution: "resolved",
        rootId: "root",
        replyToEntry: "older",
      }),
    ).toBe("root");
  });
  it("rejects a detail returned for a different account or draft", () => {
    expect(validatedDraft(compose, "A", compose.id)).toBe(true);
    expect(validatedDraft(compose, "B", compose.id)).toBe(false);
    expect(validatedDraft(compose, "A", "other")).toBe(false);
  });
});
