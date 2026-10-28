import { createElement } from "react";
import { act, create } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { useMailAction, useReadAction } from "../src/lib/inbox/mailActions";
import { appUndo } from "../src/lib/ui/undo";
import { clearToasts } from "../src/lib/ui/toasts";

async function mount() {
  let mail!: ReturnType<typeof useMailAction>;
  let read!: ReturnType<typeof useReadAction>;
  function Probe() {
    mail = useMailAction();
    read = useReadAction();
    return null;
  }
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  let tree!: ReturnType<typeof create>;
  await act(async () => {
    tree = create(createElement(QueryClientProvider, { client }, createElement(Probe)));
  });
  return {
    mail: () => mail,
    read: () => read,
    close: () => {
      act(() => tree.unmount());
      client.clear();
    },
  };
}
afterEach(() => {
  vi.unstubAllGlobals();
  clearToasts();
});

it.each(["archive", "move", "trash"] as const)(
  "undoes %s but not read-state changes",
  async (action) => {
    const calls: { url: string; body: unknown }[] = [];
    vi.stubGlobal("fetch", async (request: Request) => {
      const body = await request.json();
      calls.push({ url: new URL(request.url).pathname, body });
      const token = calls.length === 1 ? `${action}-work` : "read-personal";
      return new Response(
        JSON.stringify({
          action,
          changed: 1,
          marked: 1,
          chains: [],
          undoToken: token,
          undone: true,
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    });
    const hooks = await mount();
    const body = {
      chains: ["mail:a"],
      action,
      accountId: "work",
      ...(action === "move" ? { labels: ["Projects"] } : {}),
    };
    try {
      await act(async () => {
        await hooks.mail().mutateAsync({ body });
      });
      await act(async () => {
        await hooks
          .read()
          .mutateAsync({ body: { chain: "mail:b", unread: false, accountId: "personal" } });
      });
      expect(appUndo.getSnapshot().label).toBe(action);
      await appUndo.undo();
      expect(await appUndo.undo()).toBeUndefined();
      expect(calls).toEqual([
        {
          url: "/v1/mail",
          body: { ...body, undoable: true },
        },
        {
          url: "/v1/read",
          body: { chain: "mail:b", unread: false, accountId: "personal", undoable: false },
        },
        { url: "/v1/mail/undo", body: { token: `${action}-work` } },
      ]);
      expect(appUndo.getSnapshot().label).toBe("");
    } finally {
      hooks.close();
    }
  },
);

it.each([false, true])(
  "changes unread=%s without waiting for mailbox undo work",
  async (unread) => {
    let sent: unknown;
    vi.stubGlobal("fetch", async (request: Request) => {
      sent = await request.json();
      return new Response(JSON.stringify({ marked: 1, undoToken: "must-not-be-recorded" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    const hooks = await mount();
    let release!: () => void;
    const held = appUndo.perform(() => new Promise<void>((resolve) => (release = resolve)));
    await Promise.resolve();
    try {
      await act(async () => {
        await hooks.read().mutateAsync({
          body: { chain: "mail:b", unread, accountId: "personal", undoable: true },
        });
      });
      expect(sent).toEqual({ chain: "mail:b", unread, accountId: "personal", undoable: false });
      expect(appUndo.getSnapshot()).toEqual({ label: "", pending: true });
    } finally {
      release();
      await held;
      hooks.close();
    }
    expect(await appUndo.undo()).toBeUndefined();
  },
);

it("keeps partial-write receipts from API errors and never records an unconfirmed failure", async () => {
  let count = 0;
  vi.stubGlobal("fetch", async () => {
    count++;
    const payload =
      count === 1
        ? { error: "partial write", undoToken: "partial" }
        : count === 2
          ? { undone: true }
          : { error: "nothing written" };
    return new Response(JSON.stringify(payload), {
      status: count === 2 ? 200 : 502,
      headers: { "Content-Type": "application/json" },
    });
  });
  const hooks = await mount();
  try {
    await act(async () => {
      await expect(
        hooks.mail().mutateAsync({ body: { chains: ["mail:a"], action: "trash" } }),
      ).rejects.toThrow("partial write");
    });
    expect(appUndo.getSnapshot().label).toBe("partial trash");
    await appUndo.undo();
    await act(async () => {
      await expect(
        hooks.mail().mutateAsync({ body: { chains: ["mail:a"], action: "trash" } }),
      ).rejects.toThrow("nothing written");
    });
    expect(appUndo.getSnapshot().label).toBe("");
  } finally {
    hooks.close();
  }
});
