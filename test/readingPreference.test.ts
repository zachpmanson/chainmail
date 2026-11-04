// @vitest-environment jsdom
import { QueryClient, QueryClientProvider, notifyManager, useQuery } from "@tanstack/react-query";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Message from "../src/components/thread/Message";
import type { CorpusEntry } from "../src/lib/api/api";
import { chainsKey } from "../src/lib/api/queryKeys";
import { usePrefs } from "../src/lib/prefs/usePrefs";
import { usePreferOriginal } from "../src/lib/thread/usePreferOriginal";
import { appUndo } from "../src/lib/ui/undo";
import { corpus } from "./fixtures";

const key = [...chainsKey, { params: { path: { rootExtId: "root" } } }];
const otherKey = [...chainsKey, { params: { path: { rootExtId: "other" } } }];
const load = vi.fn(async () => ({
  html: "<p>Original</p>",
  analysis: { score: 2, matches: [], defaultsToOriginal: true, uncertain: false },
}));
let client: QueryClient;
let root: Root;
let container: HTMLDivElement;
let respond: (response: Response) => void;
let fetchMock: ReturnType<typeof vi.fn>;
const select = () =>
  container.querySelector<HTMLSelectElement>('select[aria-label="Reading style for this sender"]')!;
const cached = (queryKey = key) =>
  client.getQueryData<{ entries: CorpusEntry[] }>(queryKey)!.entries;

function Probe() {
  const { data } = useQuery({
    queryKey: key,
    queryFn: async () => ({ entries: [] as CorpusEntry[] }),
    enabled: false,
  });
  const { flip, pending } = usePreferOriginal();
  const e = data!.entries[0]!;
  return createElement(Message, {
    email: {
      sender: "Ada",
      fromEmail: "ada@example.test",
      body: "<p>Simplified</p>",
      stamp: { date: "21 Oct 2026", zone: "unknown" },
    },
    place: { id: "reading" },
    look: { orgSlot: "" },
    reading: {
      original: { extId: e.extId, load },
      person: {
        id: e.personId!,
        preferOriginal: e.preferOriginalSet ? e.preferOriginal === true : undefined,
      },
      onPreferOriginal: (next) => flip(e.personId!, next),
      pending,
    },
  });
}
async function choose(value: string) {
  await act(async () => {
    select().value = value;
    select().dispatchEvent(new Event("change", { bubbles: true }));
  });
}
async function settle(status = 200) {
  await act(async () => {
    respond(
      new Response(
        JSON.stringify(status === 200 ? { person: { personId: 1 } } : { error: "refused" }),
        { status, headers: { "Content-Type": "application/json" } },
      ),
    );
    await vi.waitFor(() => expect(client.isMutating()).toBe(0));
  });
}

beforeEach(async () => {
  notifyManager.setScheduler(queueMicrotask);
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  fetchMock = vi.fn(
    () =>
      new Promise<Response>((resolve) => {
        respond = resolve;
      }),
  );
  vi.stubGlobal("fetch", fetchMock);
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const entries = [
    corpus("root", { personId: 1, preferOriginal: true, preferOriginalSet: true }),
    corpus("reply", { personId: 1, preferOriginal: true, preferOriginalSet: true }),
    corpus("other", { personId: 2, preferOriginal: false, preferOriginalSet: true }),
  ];
  client.setQueryData(key, { entries });
  client.setQueryData(otherKey, { entries });
  usePrefs.setState({
    styledSenders: ["ada@example.test", "other@example.test"],
    plainSenders: [],
    tree: true,
    compact: true,
    listWidth: 320,
  });
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  await act(async () => {
    root.render(createElement(QueryClientProvider, { client }, createElement(Probe)));
  });
});
afterEach(async () => {
  await act(async () => root.unmount());
  client.clear();
  container.remove();
  usePrefs.setState({ styledSenders: [], plainSenders: [] });
  while (appUndo.getSnapshot().label) await appUndo.undo();
  vi.unstubAllGlobals();
  notifyManager.setScheduler((callback) => setTimeout(callback, 0));
});

describe("reading preference control", () => {
  it("offers Default / Original / Simplified and persists all three modes without changing mail undo or layout", async () => {
    const undo = vi.fn();
    await appUndo.perform(() => appUndo.remember({ label: "archive", undo }));
    const beforeUndo = appUndo.getSnapshot();
    const url = location.href;
    expect([...select().options].map((option) => option.text)).toEqual([
      "Default",
      "Original",
      "Simplified",
    ]);
    expect(select().value).toBe("original");

    for (const [mode, body, value, set] of [
      ["simplified", { preferOriginal: false }, false, true],
      ["original", { preferOriginal: true }, true, true],
      ["default", { resetOriginalPreference: true }, false, false],
    ] as const) {
      await choose(mode);
      expect(select().disabled).toBe(true);
      expect(select().value).toBe(mode);
      const request = fetchMock.mock.calls.at(-1)![0] as Request;
      expect(request.method).toBe("POST");
      expect(new URL(request.url).pathname).toBe("/v1/people/1");
      expect(await request.clone().json()).toEqual(body);
      for (const queryKey of [key, otherKey]) {
        expect(
          cached(queryKey)
            .slice(0, 2)
            .map((e) => [e.preferOriginal, e.preferOriginalSet]),
        ).toEqual([
          [value, set],
          [value, set],
        ]);
        expect(cached(queryKey)[2]).toMatchObject({
          personId: 2,
          preferOriginal: false,
          preferOriginalSet: true,
        });
      }
      await settle();
      expect(select().disabled).toBe(false);
      expect(select().value).toBe(mode);
    }
    expect(select().value).toBe("default");
    expect(container.querySelector(".bd")!.shadowRoot!.textContent).toContain("Original");
    expect(usePrefs.getState().styledSenders).toEqual(["other@example.test"]);
    expect(usePrefs.getState()).toMatchObject({ tree: true, compact: true, listWidth: 320 });
    expect(appUndo.getSnapshot()).toEqual(beforeUndo);
    expect(location.href).toBe(url);
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(undo).not.toHaveBeenCalled();
  });

  it.each(["original", "simplified"])(
    "rolls a failed %s choice back to server Default",
    async (mode) => {
      await act(async () => {
        client.setQueryData(key, {
          entries: cached().map((e) =>
            e.personId === 1 ? { ...e, preferOriginal: false, preferOriginalSet: false } : e,
          ),
        });
      });
      expect(select().value).toBe("default");
      await choose(mode);
      expect(select().value).toBe(mode);
      await settle(500);
      expect(select().value).toBe("default");
      expect(select().disabled).toBe(false);
      expect(cached()[0]).toMatchObject({ preferOriginal: false, preferOriginalSet: false });
    },
  );

  it.each([false, true])(
    "rolls a failed reset back to explicit %s, retaining fallback and unrelated cache updates",
    async (forced) => {
      await act(async () => {
        for (const queryKey of [key, otherKey])
          client.setQueryData(queryKey, {
            entries: cached(queryKey).map((e) =>
              e.personId === 1 ? { ...e, preferOriginal: forced } : e,
            ),
          });
      });
      await choose("default");
      expect(select().disabled).toBe(true);
      expect(select().value).toBe("default");
      // An unrelated update arriving during the write must survive rollback.
      await act(async () => {
        client.setQueryData(key, {
          entries: cached().map((e) => ({ ...e, subject: "changed while saving" })),
        });
      });
      await settle(400);
      expect(select().disabled).toBe(false);
      expect(select().value).toBe(forced ? "original" : "simplified");
      expect(usePrefs.getState().styledSenders).toContain("ada@example.test");
      for (const queryKey of [key, otherKey]) {
        expect(cached(queryKey)[0]).toMatchObject({
          preferOriginal: forced,
          preferOriginalSet: true,
        });
      }
      expect(cached()[0]!.subject).toBe("changed while saving");
    },
  );
});
