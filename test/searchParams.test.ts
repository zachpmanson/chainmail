import { createMemoryHistory, createRouter } from "@tanstack/react-router";
import { describe, expect, it } from "vitest";
import { router } from "../src/router";

async function searchAt(url: string) {
  const subject = createRouter({
    ...router.options,
    history: createMemoryHistory({ initialEntries: [url] }),
  });
  return subject.matchRoutes(subject.state.location).at(-1)?.search;
}

describe("textual router search parameters", () => {
  it.each(["123", "true", "false", "null", "90071992547409931234567890", "1e400"])(
    "keeps raw %s text before numeric/JSON decoding",
    async (value) => {
      const params = new URLSearchParams({
        q: value,
        label: value,
        open: value,
        accountId: value,
        person: value,
        since: value,
      });
      expect(await searchAt(`/?${params}`)).toMatchObject({
        q: value,
        label: value,
        open: value,
        accountId: value,
        person: value,
        since: value,
      });
    },
  );

  it.each([
    "123",
    "true",
    "90071992547409931234567890",
    '"quoted"',
    "[1,2]",
    '{"x":1}',
    "a+b & c",
    "",
  ])("round-trips router-generated %s text losslessly", async (value) => {
    const encoded = router.options.stringifySearch!({ q: value, label: value });
    expect(await searchAt(`/${encoded}`)).toMatchObject({ q: value, label: value });
  });

  it("accepts legacy JSON-quoted strings and manual popup=1 links", async () => {
    expect(await searchAt("/?q=%22123%22&label=%22true%22&open=123&popup=1")).toMatchObject({
      q: "123",
      label: "true",
      open: "123",
      popup: "1",
    });
    expect(await searchAt("/?open=123&popup=%221%22")).toMatchObject({ open: "123", popup: "1" });
  });

  it("still validates search modes and ops tabs", async () => {
    expect(await searchAt("/?q=true&mode=invalid")).toMatchObject({ q: "true", mode: undefined });
    expect(await searchAt("/ops?tab=merges")).toMatchObject({ tab: "merges" });
  });
});
