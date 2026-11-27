import { describe, expect, it } from "vitest";
import { canStopEditingDraft } from "../src/lib/message/draftStop";

const saved = {
  generation: 3,
  id: "draft-1",
  inputKey: "acknowledged words and options",
  acknowledgement: JSON.stringify([3, "draft-1", "acknowledged words and options"]),
  pending: false,
  failed: false,
};

describe("Stop editing draft", () => {
  it("allows closing a fully acknowledged draft", () => {
    expect(canStopEditingDraft(saved)).toBe(true);
  });

  it("refuses unacknowledged inputs or saves that are pending or uncertain", () => {
    for (const patch of [
      { inputKey: "edits still in debounce" },
      { id: "" },
      { id: "different draft" },
      { generation: 4 },
      { pending: true },
      { failed: true },
    ]) {
      expect(canStopEditingDraft({ ...saved, ...patch })).toBe(false);
    }
  });
});
