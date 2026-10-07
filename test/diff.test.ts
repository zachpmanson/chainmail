import { describe, expect, it } from "vitest";
import { diff, extractSpec } from "../src/lib/timeline/diff";
import { msg, timeline } from "./fixtures";

describe("extractSpec", () => {
  it("reads the embedded spec, unescaping </script>", () => {
    const json = JSON.stringify({
      title: "x",
      messages: [{ date: "d", body: "</script>" }],
    }).replace(/<\//g, "<\\/");
    const page = `<html><script type="application/json" id="chainmail-spec">${json}</script></html>`;
    expect(extractSpec(page).messages[0]!.body).toBe("</script>");
  });

  it("reads the old renderer's id too", () => {
    const page = `<script type="application/json" id="mt-spec">{"title":"old","messages":[]}</script>`;
    expect(extractSpec(page).title).toBe("old");
  });

  it("refuses a page with no spec", () => {
    expect(() => extractSpec("<html></html>")).toThrow(/no embedded spec/);
  });
});

describe("diff", () => {
  const base = { sender: "Ada", time: "09:00", id: undefined };

  it("marks nothing when the pages match", () => {
    const t = timeline(msg("a", base));
    expect([...diff(t, t)]).toEqual([]);
  });

  it("marks an unseen entry new and a changed body revised", () => {
    const prev = timeline(msg("a", base));
    const next = timeline(
      msg("a", { ...base, body: "<p>a, corrected</p>" }),
      msg("b", { ...base, sender: "Bo" }),
    );
    expect([...diff(prev, next)]).toEqual([
      ["m-20260302-0900-a", "revised"],
      ["m-20260302-0900-b", "new"],
    ]);
  });

  it("reads a re-timed entry with the same words as revised", () => {
    const prev = timeline(msg("a", base));
    const next = timeline(msg("a", { ...base, time: "10:00" }));
    expect([...diff(prev, next)]).toEqual([["m-20260302-1000-a", "revised"]]);
  });
});
