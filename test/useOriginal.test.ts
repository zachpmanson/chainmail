// @vitest-environment jsdom
import { createElement } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useOriginal } from "../src/lib/message/useOriginal";
import { usePrefs } from "../src/lib/prefs/usePrefs";

const designed =
  '<div class="mail"><table style="x"><tr><td><table class="x"><tr><td>News</td></tr></table></td></tr></table></div>';
const prefer = vi.fn();
type Props = {
  source: { extId: string; load: (id: string) => Promise<string> };
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
  let resolve!: (html: string) => void;
  const promise = new Promise<string>((yes) => {
    resolve = yes;
  });
  return { resolve, promise };
}
afterEach(async () => {
  await act(async () => tree?.unmount());
  tree = undefined;
  usePrefs.setState({ styledSenders: [], plainSenders: [] });
});

describe("original display lifecycle", () => {
  it("renders the original when the automatic design score selects it", async () => {
    const load = vi.fn(async () => designed);
    await render({ source: { extId: "auto", load } });
    expect(latest.on).toBe(true);
    expect(latest.state).toEqual({ at: "sent", html: designed });
    expect(load).toHaveBeenCalledTimes(1);
    expect(latest.analysis?.score).toBe(3);
    expect(latest.analysis?.defaultsToOriginal).toBe(latest.on);
  });

  it("keeps correspondence in transcript mode", async () => {
    await render({ source: { extId: "plain", load: async () => "<p>Hello</p>" } });
    expect(latest.on).toBe(false);
    expect(latest.state).toEqual({ at: "read" });
  });

  it("does not fetch or show an explicitly disabled original", async () => {
    const load = vi.fn(async () => designed);
    await render({ source: { extId: "off", load }, explicit: false });
    expect(latest.on).toBe(false);
    expect(load).not.toHaveBeenCalled();
  });

  it("loads for an expanded explanation without overriding a saved stripped view", async () => {
    const load = vi.fn(async () => designed);
    const source = { extId: "explain-off", load };
    await render({ source, explicit: false, explain: true });
    expect(latest.on).toBe(false);
    expect(latest.explicit).toBe(true);
    expect(latest.state).toEqual({ at: "read" });
    expect(latest.analysis?.score).toBe(3);
    const analysis = latest.analysis;
    await render({ source, explicit: true, explain: true });
    expect(latest.state).toEqual({ at: "sent", html: designed });
    expect(latest.analysis).toBe(analysis);
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("keeps an explicit original view even at zero points", async () => {
    await render({ source: { extId: "force-on", load: async () => "<p>Hi</p>" }, explicit: true });
    expect(latest.on).toBe(true);
    expect(latest.analysis?.score).toBe(0);
    expect(latest.analysis?.defaultsToOriginal).toBe(false);
  });

  it("excludes a signature only from analysis, not from the displayed original", async () => {
    const html = `<p>Hello</p><div class="gmail_signature">${designed}</div>`;
    await render({ source: { extId: "signature", load: async () => html }, explicit: true });
    expect(latest.analysis?.score).toBe(0);
    expect(latest.state).toEqual({ at: "sent", html });
  });

  it("preserves browser-saved stripped and original preferences too", async () => {
    usePrefs.getState().setStyled("original-test@example.test", false);
    const source = { extId: "local", load: vi.fn(async () => designed) };
    await render({ source, local: true, explain: true });
    expect(latest.on).toBe(false);
    expect(latest.explicit).toBe(true);
    expect(latest.analysis?.score).toBe(3);
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
    await act(async () => pending.resolve(designed));
    expect(latest.on).toBe(false);
    expect(latest.state).toEqual({ at: "read" });
  });

  it("does not let an earlier message response replace the current message", async () => {
    const old = deferred();
    await render({ source: { extId: "old", load: () => old.promise }, explicit: true });
    await render({ source: { extId: "new", load: async () => "<p>new</p>" }, explicit: true });
    await act(async () => old.resolve("<p>old</p>"));
    expect(latest.state).toEqual({ at: "sent", html: "<p>new</p>" });
    expect(latest.analysis?.score).toBe(0);
  });

  it("does not reuse cached HTML when its loader changes", async () => {
    await render({ source: { extId: "same", load: async () => "<p>first</p>" }, explicit: true });
    await render({ source: { extId: "same", load: async () => "<p>second</p>" }, explicit: true });
    expect(latest.state).toEqual({ at: "sent", html: "<p>second</p>" });
  });
});
