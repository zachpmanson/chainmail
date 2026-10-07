import { describe, expect, it } from "vitest";
import { resolveEdits, type EditEntry } from "../src/lib/timeline/edits";

const entries: Record<string, EditEntry> = {
  copy: { html: "<p>Due Friday, not Thursday</p>", who: "Ada", stamp: "Mon 2 Mar 2026 09:00" },
  base: { html: "<p>Due Thursday</p>", who: "Ada", stamp: "Mon 2 Mar 2026 08:00" },
};
const find = (id: string | undefined) => (id ? entries[id] : undefined);
const host = { who: "Bo", time: "10:00" };

describe("resolveEdits", () => {
  it("is undefined when nothing was edited", () => {
    expect(resolveEdits(undefined, find, host)).toBeUndefined();
    expect(resolveEdits([], find, host)).toBeUndefined();
  });

  it("credits the quoting host and names the original", () => {
    const [ed] = resolveEdits([{ id: "copy", base: "base", body: "" }], find, host)!;
    expect(ed).toMatchObject({
      base: "base",
      who: "Bo",
      time: "10:00",
      origWho: "Ada",
      origStamp: "Mon 2 Mar 2026 08:00",
    });
    expect(ed!.html.replace(/ class="[^"]*"/g, "")).toBe("<p>Due <b>Friday, not</b> Thursday</p>");
  });

  it("keeps an edit's own attribution over the host's", () => {
    const [ed] = resolveEdits([{ base: "base", who: "Cy", time: "11:30" }], find, host)!;
    expect([ed!.who, ed!.time]).toEqual(["Cy", "11:30"]);
  });

  it("leaves the original blank when its base is missing", () => {
    const [ed] = resolveEdits([{ id: "copy", body: "x" }], find, host)!;
    expect(ed).toMatchObject({ base: "", origWho: "", origStamp: "" });
  });
});
