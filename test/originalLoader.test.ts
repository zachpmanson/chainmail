import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { OriginalPayload } from "../src/lib/message/original";

const payload: OriginalPayload = {
  html: '<p id="sender-original">Hello</p>',
  analysis: {
    score: 2,
    matches: [
      { rule: "bounded or padded main layout", points: 1 },
      { rule: "designed action", points: 1 },
    ],
    defaultsToOriginal: true,
    uncertain: false,
  },
};
let load: typeof import("../src/lib/message/original").fetchOriginal;
const fetcher = vi.fn<typeof fetch>();
beforeEach(async () => {
  vi.resetModules();
  vi.stubGlobal("fetch", fetcher);
  fetcher.mockReset();
  load = (await import("../src/lib/message/original")).fetchOriginal;
});
afterEach(() => vi.unstubAllGlobals());

describe("original payload session cache", () => {
  it("shares one opt-in request and preserves the atomic server receipt", async () => {
    fetcher.mockResolvedValue(Response.json(payload));
    const a = load("mail:<one@example.test>");
    const b = load("mail:<one@example.test>");
    expect(a).toBe(b);
    expect(await a).toEqual(payload);
    expect(fetcher).toHaveBeenCalledOnce();
    expect(fetcher).toHaveBeenCalledWith(
      "/v1/entries/mail%3A%3Cone%40example.test%3E/original?analysis=1",
    );
  });

  it("evicts a rejected request so a transient error can be retried", async () => {
    fetcher.mockResolvedValueOnce(Response.json({ error: "try later" }, { status: 503 }));
    await expect(load("retry")).rejects.toMatchObject({ status: 503, message: "try later" });
    fetcher.mockResolvedValueOnce(Response.json(payload));
    expect(await load("retry")).toEqual(payload);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("keeps 404 unavailable rather than fabricating zero analysis", async () => {
    fetcher.mockResolvedValue(Response.json({ error: "no such original" }, { status: 404 }));
    await expect(load("missing")).rejects.toMatchObject({ status: 404 });
  });

  it.each([
    { html: payload.html },
    { html: payload.html, analysis: { ...payload.analysis, matches: null } },
    { html: payload.html, analysis: { ...payload.analysis, score: -1 } },
    { html: payload.html, analysis: { ...payload.analysis, defaultsToOriginal: "true" } },
  ])("keeps available HTML but treats a missing/malformed receipt as unavailable", async (body) => {
    fetcher.mockResolvedValue(Response.json(body));
    expect(await load("unavailable")).toEqual({ html: payload.html, analysis: undefined });
  });

  it("accepts legacy sanitized HTML without inventing analysis", async () => {
    fetcher.mockResolvedValue(
      new Response(payload.html, { headers: { "Content-Type": "text/html; charset=utf-8" } }),
    );
    expect(await load("legacy")).toEqual({ html: payload.html });
  });

  it("evicts malformed JSON and a missing HTML response", async () => {
    fetcher.mockResolvedValueOnce(
      new Response("{broken", { headers: { "Content-Type": "application/json" } }),
    );
    await expect(load("bad-json")).rejects.toThrow();
    fetcher.mockResolvedValueOnce(Response.json({ analysis: payload.analysis }));
    await expect(load("bad-json")).rejects.toThrow("no HTML");
    fetcher.mockResolvedValueOnce(Response.json(payload));
    expect(await load("bad-json")).toEqual(payload);
  });
});
