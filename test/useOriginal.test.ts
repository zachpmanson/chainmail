import { createElement } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useOriginal } from "../src/lib/message/useOriginal";

const designed = '<div class="mail"><table style="x"></table><table class="x"></table></div>';
const prefer = vi.fn();
type Props = {
  source: { extId: string; load: (id: string) => Promise<string> };
  explicit?: boolean;
};
let tree: ReactTestRenderer | undefined;
let latest: ReturnType<typeof useOriginal>;
function Probe({ source, explicit }: Props) {
  latest = useOriginal(
    source,
    "original-test@example.test",
    { id: 1, preferOriginal: explicit },
    prefer,
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
});

describe("original display lifecycle", () => {
  it("renders the original when the automatic design score selects it", async () => {
    const load = vi.fn(async () => designed);
    await render({ source: { extId: "auto", load } });
    expect(latest.on).toBe(true);
    expect(latest.state).toEqual({ at: "sent", html: designed });
    expect(load).toHaveBeenCalledTimes(1);
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
  });

  it("does not reuse cached HTML when its loader changes", async () => {
    await render({ source: { extId: "same", load: async () => "<p>first</p>" }, explicit: true });
    await render({ source: { extId: "same", load: async () => "<p>second</p>" }, explicit: true });
    expect(latest.state).toEqual({ at: "sent", html: "<p>second</p>" });
  });
});
