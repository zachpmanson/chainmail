// @vitest-environment jsdom
import { createElement } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useOriginal } from "../src/lib/message/useOriginal";
import type { OriginalPayload } from "../src/lib/message/original";
import { usePrefs } from "../src/lib/prefs/usePrefs";

const designed =
  '<div class="mail"><table style="x"><tr><td><table class="x"><tr><td>News</td></tr></table></td></tr></table></div>';
const positive: OriginalPayload = {
  html: designed,
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
const plain = (html: string): OriginalPayload => ({
  html,
  analysis: { score: 0, matches: [], defaultsToOriginal: false, uncertain: false },
});
const prefer = vi.fn();
type Props = {
  source: { extId: string; load: (id: string) => Promise<OriginalPayload> };
  explicit?: boolean;
  explain?: boolean;
  local?: boolean;
};
let tree: ReactTestRenderer | undefined;
let latest: ReturnType<typeof useOriginal>;
function Probe({ source, explicit, explain, local }: Props) {
  latest = useOriginal(
    source,
    "original-test@example.test",
    { id: 1, preferOriginal: explicit },
    local ? undefined : prefer,
    explain,
  );
  return null;
}
async function render(props: Props) {
  await act(async () => {
    if (tree) tree.update(createElement(Probe, props));
    else tree = create(createElement(Probe, props));
  });
}
function deferred() {
  let resolve!: (html: OriginalPayload) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<OriginalPayload>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { resolve, reject, promise };
}
afterEach(async () => {
  await act(async () => tree?.unmount());
  tree = undefined;
  usePrefs.setState({ styledSenders: [], plainSenders: [] });
});

describe("original display lifecycle", () => {
  it("renders the original when the automatic design score selects it", async () => {
    const load = vi.fn(async () => positive);
    await render({ source: { extId: "auto", load } });
    expect(latest.on).toBe(true);
    expect(latest.state).toEqual({ at: "sent", html: designed });
    expect(load).toHaveBeenCalledTimes(1);
    expect(latest.analysis?.score).toBe(2);
    expect(latest.analysis?.defaultsToOriginal).toBe(latest.on);
  });

  it("keeps correspondence in transcript mode", async () => {
    await render({ source: { extId: "plain", load: async () => plain("<p>Hello</p>") } });
    expect(latest.on).toBe(false);
    expect(latest.state).toEqual({ at: "read" });
  });

  it("does not fetch or show an explicitly disabled original", async () => {
    const load = vi.fn(async () => positive);
    await render({ source: { extId: "off", load }, explicit: false });
    expect(latest.on).toBe(false);
    expect(load).not.toHaveBeenCalled();
  });

  it("loads for an expanded explanation without overriding a saved stripped view", async () => {
    const load = vi.fn(async () => positive);
    const source = { extId: "explain-off", load };
    await render({ source, explicit: false, explain: true });
    expect(latest.on).toBe(false);
    expect(latest.explicit).toBe(true);
    expect(latest.state).toEqual({ at: "read" });
    expect(latest.analysis?.score).toBe(2);
    const analysis = latest.analysis;
    await render({ source, explicit: true, explain: true });
    expect(latest.state).toEqual({ at: "sent", html: designed });
    expect(latest.analysis).toBe(analysis);
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("keeps an explicit original view even at zero points", async () => {
    await render({
      source: { extId: "force-on", load: async () => plain("<p>Hi</p>") },
      explicit: true,
    });
    expect(latest.on).toBe(true);
    expect(latest.analysis?.score).toBe(0);
    expect(latest.analysis?.defaultsToOriginal).toBe(false);
  });

  it("uses the supplied receipt without rewriting a distinct displayed original", async () => {
    const html = `<p>Hello</p><div class="gmail_signature">${designed}</div>`;
    await render({ source: { extId: "signature", load: async () => plain(html) }, explicit: true });
    expect(latest.analysis?.score).toBe(0);
    expect(latest.state).toEqual({ at: "sent", html });
  });

  it("preserves browser-saved stripped and original preferences too", async () => {
    usePrefs.getState().setStyled("original-test@example.test", false);
    const source = { extId: "local", load: vi.fn(async () => positive) };
    await render({ source, local: true, explain: true });
    expect(latest.on).toBe(false);
    expect(latest.explicit).toBe(true);
    expect(latest.analysis?.score).toBe(2);
    await act(async () => latest.ask());
    expect(latest.on).toBe(true);
    expect(latest.state).toEqual({ at: "sent", html: designed });
    expect(source.load).toHaveBeenCalledTimes(1);
  });

  it("ignores a pending automatic response after an explicit switch off", async () => {
    const pending = deferred();
    const source = { extId: "toggle", load: () => pending.promise };
    await render({ source });
    await render({ source, explicit: false });
    await act(async () => pending.resolve(positive));
    expect(latest.on).toBe(false);
    expect(latest.state).toEqual({ at: "read" });
  });

  it("does not let an earlier message response replace the current message", async () => {
    const old = deferred();
    await render({ source: { extId: "old", load: () => old.promise }, explicit: true });
    await render({
      source: { extId: "new", load: async () => plain("<p>new</p>") },
      explicit: true,
    });
    await act(async () => old.resolve(plain("<p>old</p>")));
    expect(latest.state).toEqual({ at: "sent", html: "<p>new</p>" });
    expect(latest.analysis?.score).toBe(0);
  });

  it("uses the supplied positive receipt even when displayed HTML is ordinary prose", async () => {
    const payload = { ...positive, html: "<p>Ordinary display HTML</p>" };
    await render({ source: { extId: "server-positive", load: async () => payload } });
    expect(latest.on).toBe(true);
    expect(latest.analysis).toBe(payload.analysis);
    expect(latest.state).toEqual({ at: "sent", html: payload.html });
  });

  it("does not rescore elaborate display HTML when the server says simplified", async () => {
    await render({ source: { extId: "server-negative", load: async () => plain(designed) } });
    expect(latest.on).toBe(false);
    expect(latest.analysis?.score).toBe(0);
  });

  it("keeps missing analysis unavailable while explicit original may display available HTML", async () => {
    const source = { extId: "legacy", load: vi.fn(async () => ({ html: designed })) };
    await render({ source });
    expect(latest.on).toBe(false);
    expect(latest.analysis).toBeUndefined();
    await render({ source, explicit: true });
    expect(latest.state).toEqual({ at: "sent", html: designed });
    expect(latest.analysis).toBeUndefined();
    expect(source.load).toHaveBeenCalledTimes(1);
  });

  it("clears a previous receipt while a new message is pending, and ignores late responses", async () => {
    const old = deferred();
    const newer = deferred();
    await render({ source: { extId: "old", load: () => old.promise } });
    await render({ source: { extId: "new", load: () => newer.promise } });
    expect(latest.analysis).toBeUndefined();
    expect(latest.analysisState).toBe("asking");
    await act(async () => old.resolve(positive));
    expect(latest.analysis).toBeUndefined();
    await act(async () => newer.resolve(plain("<p>New</p>")));
    expect(latest.on).toBe(false);
    expect(latest.analysis?.score).toBe(0);
  });

  it("ignores a stale loader's positive response and rejection at the same extId", async () => {
    const old = deferred();
    const source = { extId: "same", load: async () => plain("<p>Current</p>") };
    await render({ source: { extId: "same", load: () => old.promise } });
    await render({ source });
    await act(async () => old.resolve(positive));
    expect(latest.on).toBe(false);
    expect(latest.analysis?.score).toBe(0);
    const rejected = deferred();
    await render({ source: { extId: "same", load: () => rejected.promise } });
    await render({ source });
    await act(async () => rejected.reject(new Error("old failure")));
    expect(latest.analysisState).toBe("read");
    expect(latest.analysis?.score).toBe(0);
  });

  it("preserves explicit choices on failure and while explanation-only analysis is pending", async () => {
    const fail = vi.fn(async (): Promise<OriginalPayload> => {
      throw new Error("unavailable");
    });
    await render({ source: { extId: "failed", load: fail }, explicit: true });
    expect(latest.on).toBe(true);
    expect(latest.analysis).toBeUndefined();
    expect(latest.state).toEqual({ at: "none", why: "unavailable" });
    const pending = deferred();
    const source = { extId: "explain-pending", load: () => pending.promise };
    await render({ source, explain: true });
    await render({ source, explicit: false, explain: true });
    await act(async () => pending.resolve(positive));
    expect(latest.on).toBe(false);
    expect(latest.analysis).toBe(positive.analysis);
    expect(latest.state).toEqual({ at: "read" });
  });

  it("does not reuse cached HTML when its loader changes", async () => {
    await render({
      source: { extId: "same", load: async () => plain("<p>first</p>") },
      explicit: true,
    });
    await render({
      source: { extId: "same", load: async () => plain("<p>second</p>") },
      explicit: true,
    });
    expect(latest.state).toEqual({ at: "sent", html: "<p>second</p>" });
  });
});
