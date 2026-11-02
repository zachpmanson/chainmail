import { createElement, type ComponentProps } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ComposeBox from "../src/components/compose/ComposeBox";
import ReplyBox from "../src/components/compose/ReplyBox";
import ComposeForm from "../src/components/compose/ComposeForm";
import ComposerFields from "../src/components/compose/ComposerFields";
import { makeQueryClient } from "../src/lib/api/queryClient";
import type { CorpusEntry } from "../src/lib/api/api";

vi.mock("../src/lib/inbox/useAccountId", () => ({ useAccountId: () => "acct" }));
vi.mock("../src/components/compose/useAccounts", () => ({
  default: () => ({ connected: [{ id: "acct", email: "me@example.test" }] }),
}));
vi.mock("../src/lib/message/who", () => {
  const people = new Map();
  return { usePersonAddresses: () => people, addressesOf: () => [] };
});
// Keep the actual reducers, recipient hook, busy hook, Query caching and HTTP mutations.
// Only visual children are replaced with observable controls.
vi.mock("../src/components/compose/ComposeForm", () => ({
  default: (props: ComponentProps<typeof ComposeForm>) => createElement("compose-form", props),
}));
vi.mock("../src/components/compose/ComposerFields", () => ({
  default: (props: ComponentProps<typeof ComposerFields>) =>
    createElement("composer-fields", props),
}));
vi.mock("../src/components/compose/ReplyTarget", () => ({ default: () => null }));
vi.mock("../src/components/compose/ReplyPlan", () => ({
  default: (props: object) => createElement("reply-plan", props),
}));
vi.mock("../src/components/compose/ComposePreview", () => ({
  default: (props: object) => createElement("compose-preview", props),
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
const json = (data: unknown) =>
  new Response(JSON.stringify(data), {
    headers: { "Content-Type": "application/json" },
  });
const saved = {
  accountId: "acct",
  id: "d1",
  messageId: "m1",
  threadId: "thread-1",
  to: "reply-target@example.test",
  cc: "",
  body: "OLD SAVED WORDS",
  subject: "Re: Hello",
};
const answer = {
  extId: "A",
  container: "thread-1",
  fromEmail: "from@example.test",
  subject: "Hello",
} as CorpusEntry;
const replyProps = {
  thread: { rootExtId: "root" },
  answer,
  answerAnchor: "A",
  words: { who: "Sender", when: "today" },
  all: true,
  onAll: vi.fn(),
  aimed: 0,
};
let root: ReactTestRenderer;
let client: QueryClient;
let requests: { path: string; method: string; body: Record<string, unknown> }[];
let handle: (request: Request) => Promise<Response>;

async function settle() {
  for (let i = 0; i < 5; i++)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
}
async function mount(reply = true) {
  await act(async () => {
    root = create(
      createElement(
        QueryClientProvider,
        { client },
        reply
          ? createElement(ReplyBox, replyProps)
          : createElement(ComposeBox, { onClose: vi.fn() }),
      ),
      { createNodeMock: () => null, unstable_isConcurrent: true } as Parameters<typeof create>[1],
    );
  });
  await settle();
}
const fields = () =>
  root.root.findByType("composer-fields" as never).props as ComponentProps<typeof ComposerFields>;
const form = () =>
  root.root.findByType("compose-form" as never).props as ComponentProps<typeof ComposeForm>;
function button(text: string) {
  return root.root.findAllByType("button").find((node) => node.children.join("") === text)!;
}
function visibleText() {
  return root.root
    .findAll((node) => typeof node.type === "string")
    .flatMap((node) => node.children.filter((child) => typeof child === "string"))
    .join(" ");
}
async function click(text: string) {
  expect(button(text).props.disabled).not.toBe(true);
  await act(async () => {
    button(text).props.onClick();
  });
  await settle();
}
async function typeReply(body: string) {
  await act(async () => {
    fields().onDraft({ body });
  });
  await settle();
}
async function autosave() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(701);
  });
  await settle();
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.stubGlobal("window", { setTimeout, clearTimeout, confirm: () => true });
  client = makeQueryClient();
  requests = [];
  handle = async (req) => {
    const path = new URL(req.url).pathname;
    if (path === "/v1/drafts" && req.method === "GET") return json([]);
    if (path === "/v1/drafts" || req.method === "PUT") return json(saved);
    if (req.method === "DELETE") return json({ deleted: true });
    if (path === "/v1/send")
      return json({
        accountId: "acct",
        entry: "A",
        body: "reviewed reply",
        to: "reply-target@example.test",
        toRecipients: [{ address: "reply-target@example.test" }],
        ccRecipients: [],
      });
    return json({});
  };
  vi.stubGlobal("fetch", async (req: Request) => {
    const body = req.method === "GET" || req.method === "DELETE" ? {} : await req.clone().json();
    requests.push({ path: new URL(req.url).pathname, method: req.method, body });
    return handle(req);
  });
});
afterEach(async () => {
  if (root)
    await act(async () => {
      root.unmount();
    });
  client.clear();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it.each([true, false])(
  "shows draft-list failure, preserves typed text, and resumes autosave after a safe retry (reply: %s)",
  async (reply) => {
    client.setDefaultOptions({ queries: { retry: false } });
    const normal = handle;
    let unavailable = true;
    handle = async (req) => {
      if (req.method === "GET" && new URL(req.url).pathname === "/v1/drafts" && unavailable)
        return new Response(JSON.stringify({ error: "Draft decoding failed" }), {
          status: 502,
          headers: { "Content-Type": "application/json" },
        });
      return normal(req);
    };
    await mount(reply);
    if (reply) await typeReply("Keep these unsaved words");
    else
      await act(async () => {
        form().onDraft({ body: "Keep these unsaved words" });
      });
    await autosave();
    expect(visibleText()).toContain("Autosave is paused");
    expect(visibleText()).toContain("Copy your text before leaving");
    expect(visibleText()).toContain("Draft decoding failed");
    expect(requests.filter((request) => request.method !== "GET")).toEqual([]);
    expect(reply ? fields().draft.body : form().draft.body).toBe("Keep these unsaved words");
    unavailable = false;
    await click("Retry draft check");
    expect(visibleText()).not.toContain("Autosave is paused");
    expect(reply ? fields().draft.body : form().draft.body).toBe("Keep these unsaved words");
    await autosave();
    const writes = requests.filter(
      (request) => request.method === "POST" && request.path === "/v1/drafts",
    );
    expect(writes).toHaveLength(1);
    expect(writes[0]!.body.body).toBe("Keep these unsaved words");
  },
);

it.each([true, false])(
  "does not replay an uncertain draft creation through autosave (reply: %s)",
  async (reply) => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const normal = handle;
    handle = (req) => {
      if (req.method === "POST" && new URL(req.url).pathname === "/v1/drafts") {
        // Gmail may have created it; the browser never received its ID.
        return Promise.reject(new TypeError("Failed to fetch after draft creation"));
      }
      return normal(req);
    };
    await mount(reply);
    if (reply) await typeReply("New words");
    else
      await act(async () => {
        form().onDraft({ to: [{ address: "to@example.test" }], body: "New words" });
      });
    await autosave();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    if (reply) await typeReply("An additional edit must not repeat the unknown create");
    else
      await act(async () => {
        form().onDraft({ body: "An additional edit must not repeat the unknown create" });
      });
    await autosave();
    expect(requests.filter((r) => r.method === "POST" && r.path === "/v1/drafts")).toHaveLength(1);
  },
);

const draftWrites = () =>
  requests.filter(
    (r) =>
      (r.method === "POST" && r.path === "/v1/drafts") ||
      (r.method === "PUT" && r.path.startsWith("/v1/drafts/")),
  );
async function editWords(reply: boolean, body: string) {
  if (reply) await typeReply(body);
  else
    await act(async () => {
      form().onDraft({ to: [{ address: "to@example.test" }], body });
    });
}

describe("acknowledged autosave input", () => {
  it.each([true, false])(
    "does not show an old acknowledgement while different input is still being written (reply: %s)",
    async (reply) => {
      await mount(reply);
      await editWords(reply, "Payload A");
      await autosave();
      expect(visibleText()).toContain("Saved to Gmail");
      const pending = deferred<Response>();
      const normal = handle;
      handle = (req) => (req.method === "PUT" ? pending.promise : normal(req));
      await editWords(reply, "Payload B");
      await autosave();
      expect(draftWrites()).toHaveLength(2);
      await editWords(reply, "Payload A");
      expect(visibleText()).not.toContain("Saved to Gmail");
      handle = normal;
      await act(async () => pending.resolve(json({ ...saved, messageId: "revision-B" })));
      await settle();
      expect(visibleText()).not.toContain("Saved to Gmail");
      await autosave();
      expect(draftWrites().map((request) => [request.method, request.body.body])).toEqual([
        ["POST", "Payload A"],
        ["PUT", "Payload B"],
        ["PUT", "Payload A"],
      ]);
      expect(draftWrites()[2]!.body.expectedMessageId).toBe("revision-B");
      expect(visibleText()).toContain("Saved to Gmail");
    },
  );

  it.each([true, false])(
    "stays idle after acknowledgement and writes exactly once for a new edit (reply: %s)",
    async (reply) => {
      await mount(reply);
      await editWords(reply, "Stable idle fixture words");
      await autosave();
      expect(draftWrites()).toHaveLength(1);
      expect(visibleText()).toContain("Saved to Gmail");
      await act(async () => {
        await vi.advanceTimersByTimeAsync(6_000);
        await client.invalidateQueries({ queryKey: ["get", "/v1/drafts"] });
      });
      await settle();
      await editWords(reply, "Stable idle fixture words");
      await autosave();
      expect(draftWrites()).toHaveLength(1);
      await editWords(reply, "One genuine edit");
      expect(visibleText()).not.toContain("Saved to Gmail");
      await autosave();
      expect(draftWrites().map((r) => [r.method, r.body.body])).toEqual([
        ["POST", "Stable idle fixture words"],
        ["PUT", "One genuine edit"],
      ]);
      expect(visibleText()).toContain("Saved to Gmail");
      await act(async () => {
        await vi.advanceTimersByTimeAsync(6_000);
      });
      await settle();
      expect(draftWrites()).toHaveLength(2);
    },
  );

  it.each([
    [true, "POST"],
    [false, "POST"],
    [true, "PUT"],
    [false, "PUT"],
  ] as const)(
    "skips duplicate input queued behind a slow successful save (reply: %s, method: %s)",
    async (reply, method) => {
      await mount(reply);
      if (method === "PUT") {
        await editWords(reply, "Initial words");
        await autosave();
      }
      const pending = deferred<Response>();
      const normal = handle;
      handle = (req) => (req.method === method ? pending.promise : normal(req));
      await editWords(reply, "Same pending words");
      await autosave();
      const count = draftWrites().length;
      for (let i = 0; i < 2; i++) {
        await editWords(reply, "Same pending words");
        await autosave();
      }
      expect(draftWrites()).toHaveLength(count);
      await act(async () => {
        pending.resolve(json({ ...saved, messageId: "m2" }));
      });
      await settle();
      expect(draftWrites()).toHaveLength(count);
      expect(visibleText()).toContain("Saved to Gmail");
    },
  );

  it.each([
    [true, "POST"],
    [false, "POST"],
    [true, "PUT"],
    [false, "PUT"],
  ] as const)(
    "does not label newer input Saved when an older save is acknowledged (reply: %s, method: %s)",
    async (reply, method) => {
      await mount(reply);
      if (method === "PUT") {
        await editWords(reply, "Initial words");
        await autosave();
      }
      const pending = deferred<Response>();
      const normal = handle;
      let writes = 0;
      handle = (req) => (req.method === method && ++writes === 1 ? pending.promise : normal(req));
      await editWords(reply, "Older in-flight words");
      await autosave();
      const count = draftWrites().length;
      await editWords(reply, "Newer unsaved words");
      await act(async () => {
        pending.resolve(json(saved));
      });
      await settle();
      expect(draftWrites()).toHaveLength(count);
      expect(visibleText()).not.toContain("Saved to Gmail");
      expect(reply ? fields().draft.body : form().draft.body).toBe("Newer unsaved words");
      await autosave();
      expect(draftWrites()).toHaveLength(count + 1);
      expect(draftWrites()[count]!.body.body).toBe("Newer unsaved words");
      expect(visibleText()).toContain("Saved to Gmail");
    },
  );

  it.each([true, false])(
    "saves identical words again after discard resets the draft owner (reply: %s)",
    async (reply) => {
      await mount(reply);
      await editWords(reply, "Identical words in a new draft");
      await autosave();
      await click("Discard draft");
      expect(visibleText()).not.toContain("Saved to Gmail");
      const normal = handle;
      handle = (req) =>
        req.method === "POST" && new URL(req.url).pathname === "/v1/drafts"
          ? Promise.resolve(json({ ...saved, id: "d2", messageId: "m2" }))
          : normal(req);
      await editWords(reply, "Identical words in a new draft");
      await autosave();
      expect(draftWrites().map((r) => r.method)).toEqual(["POST", "POST"]);
      expect(visibleText()).toContain("Saved to Gmail");
    },
  );

  it.each([true, false])(
    "does not reuse an acknowledgement across reply targets or sending accounts (reply: %s)",
    async (reply) => {
      await mount(reply);
      await editWords(reply, "Words that stay when scope changes");
      await autosave();
      if (reply) {
        await act(async () => {
          root.update(
            createElement(
              QueryClientProvider,
              { client },
              createElement(ReplyBox, {
                ...replyProps,
                answer: { ...answer, extId: "B", container: "thread-2" },
              }),
            ),
          );
        });
        await settle();
      } else {
        await act(async () => {
          form().onAccount("other-account");
        });
        await settle();
      }
      expect(visibleText()).not.toContain("Saved to Gmail");
      await autosave();
      expect(draftWrites()).toHaveLength(2);
      expect(draftWrites()[1]!.method).toBe("POST");
      expect(draftWrites()[1]!.body).toMatchObject({
        body: "Words that stay when scope changes",
        ...(reply ? { replyToEntry: "B" } : { accountId: "other-account" }),
      });
    },
  );

  it("does not reuse a compose acknowledgement for another selected draft with identical content", async () => {
    const original = {
      ...saved,
      threadId: "",
      to: "to@example.test",
      subject: "Same subject",
      body: "Same words",
    };
    const normal = handle;
    handle = (req) =>
      req.method === "GET" && new URL(req.url).pathname === "/v1/drafts"
        ? Promise.resolve(json([original, { ...original, id: "d2", messageId: "m2" }]))
        : req.method === "PUT"
          ? Promise.resolve(json({ ...original, id: new URL(req.url).pathname.split("/").at(-1) }))
          : normal(req);
    await mount(false);
    for (const id of ["d1", "d2"]) {
      await act(async () => {
        root.root.findByType("select").props.onChange({ target: { value: id } });
      });
      expect(visibleText()).not.toContain("Saved to Gmail");
      await autosave();
      expect(visibleText()).toContain("Saved to Gmail");
    }
    expect(draftWrites().map((r) => r.path)).toEqual(["/v1/drafts/d1", "/v1/drafts/d2"]);
  });
});

describe("queued autosave outcomes", () => {
  async function edit(reply: boolean, body: string) {
    if (reply) await typeReply(body);
    else
      await act(async () => {
        form().onDraft({ to: [{ address: "to@example.test" }], body });
      });
  }

  it.each([
    [true, "network"],
    [false, "network"],
    [true, "502"],
    [false, "502"],
  ] as const)(
    "cancels queued saves after a slow uncertain create (reply: %s, failure: %s)",
    async (reply, failure) => {
      vi.spyOn(console, "error").mockImplementation(() => {});
      const first = deferred<Response>();
      let creates = 0;
      const normal = handle;
      handle = (req) => {
        if (req.method === "POST" && new URL(req.url).pathname === "/v1/drafts") {
          return ++creates === 1 ? first.promise : Promise.resolve(json(saved));
        }
        return normal(req);
      };
      await mount(reply);
      await edit(reply, "First words");
      await autosave();
      expect(creates).toBe(1);
      for (const body of ["Edited while create is pending", "Another queued edit"]) {
        await edit(reply, body);
        await autosave();
        expect(creates).toBe(1);
      }
      await act(async () => {
        if (failure === "network")
          first.reject(new TypeError("Response lost after draft creation"));
        else
          first.resolve(
            new Response(JSON.stringify({ error: "Unknown create outcome" }), {
              status: 502,
              headers: { "Content-Type": "application/json" },
            }),
          );
      });
      await settle();
      await edit(reply, "An edit after failure must not restart autosave");
      await autosave();
      expect(creates).toBe(1);
      expect(requests.filter((r) => r.method === "PUT")).toEqual([]);
    },
  );

  it.each([true, false])(
    "continues queued saves after a slow successful create (reply: %s)",
    async (reply) => {
      const first = deferred<Response>();
      const normal = handle;
      handle = (req) =>
        req.method === "POST" && new URL(req.url).pathname === "/v1/drafts"
          ? first.promise
          : normal(req);
      await mount(reply);
      await edit(reply, "First words");
      await autosave();
      await edit(reply, "Newer queued words");
      await autosave();
      expect(requests.filter((r) => r.method === "PUT")).toEqual([]);
      await act(async () => {
        first.resolve(json(saved));
      });
      await settle();
      expect(requests.filter((r) => r.method === "POST" && r.path === "/v1/drafts")).toHaveLength(
        1,
      );
      expect(
        requests
          .filter((r) => r.method === "PUT")
          .map((r) => ({ path: r.path, body: r.body.body, revision: r.body.expectedMessageId })),
      ).toEqual([{ path: "/v1/drafts/d1", body: "Newer queued words", revision: "m1" }]);
    },
  );
});

describe("reply draft ownership", () => {
  it("does not hydrate over words typed before initial draft discovery", async () => {
    const list = deferred<Response>();
    const normal = handle;
    handle = (req) =>
      req.method === "GET" && new URL(req.url).pathname === "/v1/drafts"
        ? list.promise
        : normal(req);
    await mount();
    await typeReply("NEW UNSAVED WORDS");
    await act(async () => {
      list.resolve(json([saved]));
    });
    await settle();
    expect(fields().draft.body).toBe("NEW UNSAVED WORDS");
    await autosave();
    expect(requests.filter((r) => r.path.startsWith("/v1/drafts") && r.method !== "GET")).toEqual(
      [],
    );
    await click("Resume saved draft");
    expect(fields().draft.body).toBe("OLD SAVED WORDS");
  });

  it.each(["discard", "send"])(
    "retires a completed draft on %s, even if stale refetches return it",
    async (action) => {
      const refresh = deferred<Response>();
      let reads = 0;
      const normal = handle;
      handle = (req) =>
        req.method === "GET" && new URL(req.url).pathname === "/v1/drafts"
          ? ++reads === 1
            ? Promise.resolve(json([saved]))
            : refresh.promise
          : normal(req);
      await mount();
      expect(fields().draft.body).toBe("OLD SAVED WORDS");
      if (action === "discard") await click("Discard draft");
      else {
        await click("preview");
        await click("send this reply");
      }
      expect(fields().draft.body).toBe("");
      expect(client.getQueriesData({ queryKey: ["get", "/v1/drafts"] })[0]?.[1]).toEqual([]);
      await act(async () => {
        refresh.resolve(json([saved]));
      });
      await settle();
      expect(fields().draft.body).toBe("");
      const completedRequests = requests.length;
      await typeReply("A genuinely new reply");
      await autosave();
      const newRequests = requests.slice(completedRequests);
      expect(newRequests.some((r) => r.method === "PUT" && r.path === "/v1/drafts/d1")).toBe(false);
      expect(newRequests.some((r) => r.method === "POST" && r.path === "/v1/drafts")).toBe(true);
    },
  );

  it.each([false, true])(
    "keeps a discard waiting on autosave bound to its original owner (retarget: %s)",
    async (retarget) => {
      const put = deferred<Response>();
      const normal = handle;
      handle = (req) => {
        const path = new URL(req.url).pathname;
        if (path === "/v1/drafts" && req.method === "GET") return Promise.resolve(json([saved]));
        if (req.method === "PUT") return put.promise;
        return normal(req);
      };
      await mount();
      await autosave();
      expect(requests.filter((r) => r.method === "PUT")).toHaveLength(1);
      await click("Discard draft");
      if (retarget) {
        await act(async () => {
          root.update(
            createElement(
              QueryClientProvider,
              { client },
              createElement(ReplyBox, {
                ...replyProps,
                answer: { ...answer, extId: "B", container: "thread-2" },
              }),
            ),
          );
        });
        await settle();
      }
      await act(async () => {
        put.resolve(json({ ...saved, messageId: "m2" }));
      });
      await settle();
      expect(fields().draft.body).toBe(retarget ? "OLD SAVED WORDS" : "");
      expect(requests.filter((r) => r.method === "DELETE").map((r) => r.path)).toEqual(
        retarget ? [] : ["/v1/drafts/d1"],
      );
    },
  );

  it("omits untouched recipient estimates from autosave, preserving explicit edits", async () => {
    await mount();
    await typeReply("Reply to Reply-To, not corpus From");
    await autosave();
    const first = requests.find((r) => r.method === "POST" && r.path === "/v1/drafts")!;
    expect(first.body).not.toHaveProperty("to");
    expect(first.body).not.toHaveProperty("cc");
    expect(first.body.replyAll).toBe(true);
    await act(async () => {
      fields().onDraft({ cc: [] });
    });
    await autosave();
    const update = requests.find((r) => r.method === "PUT")!;
    expect(update.body).not.toHaveProperty("to");
    expect(update.body.cc).toEqual([]);
  });
});

it("resumes canonical mailbox recipients without changing Reply-To to corpus From", async () => {
  await mount();
  await typeReply("A reply for the Reply-To target");
  await autosave();
  await act(async () => {
    root.unmount();
  });
  client.removeQueries({ queryKey: ["get", "/v1/drafts"] });
  const normal = handle;
  handle = (req) =>
    req.method === "GET" && new URL(req.url).pathname === "/v1/drafts"
      ? Promise.resolve(json([saved]))
      : normal(req);
  await mount();
  expect(fields().draft.to.map((r) => r.address)).toEqual(["reply-target@example.test"]);
  await click("preview");
  expect(requests.find((r) => r.path === "/v1/send")?.body.to).toEqual([
    "reply-target@example.test",
  ]);
});

describe("reply draft send safety", () => {
  it.each([false, true])(
    "refuses send and cleanup for an unsupported resumed draft (autosaved: %s)",
    async (autosaved) => {
      vi.spyOn(console, "error").mockImplementation(() => {});
      const normal = handle;
      handle = (req) => {
        const path = new URL(req.url).pathname;
        if (path === "/v1/drafts" && req.method === "GET") return Promise.resolve(json([saved]));
        if (req.method === "PUT")
          return Promise.resolve(
            new Response(
              JSON.stringify({
                error:
                  "This draft contains headers or MIME content Chainmail cannot preserve; edit it in Gmail",
              }),
              { status: 409, headers: { "Content-Type": "application/json" } },
            ),
          );
        return normal(req);
      };
      await mount();
      if (autosaved) await autosave();
      await click("preview");
      await click("send this reply");
      expect(requests.filter((r) => r.body.confirm === true)).toEqual([]);
      expect(requests.filter((r) => r.method === "DELETE")).toEqual([]);
      expect(visibleText()).toContain("edit it in Gmail");
      expect(visibleText()).not.toContain("Check Gmail before attempting to send again.");
    },
  );

  it("can send a supported draft after a recovered autosave error passes the authoritative preflight", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    let puts = 0;
    const normal = handle;
    handle = (req) => {
      const path = new URL(req.url).pathname;
      if (path === "/v1/drafts" && req.method === "GET") return Promise.resolve(json([saved]));
      if (req.method === "PUT")
        return Promise.resolve(
          ++puts === 1
            ? new Response(JSON.stringify({ error: "Temporary mailbox failure" }), {
                status: 502,
                headers: { "Content-Type": "application/json" },
              })
            : json({ ...saved, messageId: "m2" }),
        );
      return normal(req);
    };
    await mount();
    await autosave();
    expect(puts).toBe(1);
    await click("preview");
    await click("send this reply");
    expect(puts).toBe(2);
    expect(requests.filter((r) => r.body.confirm === true)).toHaveLength(1);
    expect(requests.filter((r) => r.method === "DELETE")).toHaveLength(1);
    expect(visibleText()).not.toContain("Temporary mailbox failure");
    const preflight = requests.filter((r) => r.method === "PUT")[1]!;
    expect(preflight.body).toMatchObject({
      expectedMessageId: "m1",
      replyToEntry: "A",
      body: "OLD SAVED WORDS",
    });
  });
});

it("does not send or clean up if the reply owner changes during draft preflight", async () => {
  const preflight = deferred<Response>();
  const normal = handle;
  handle = (req) => {
    if (req.method === "GET" && new URL(req.url).pathname === "/v1/drafts")
      return Promise.resolve(json([saved]));
    if (req.method === "PUT") return preflight.promise;
    return normal(req);
  };
  await mount();
  await click("preview");
  await click("send this reply");
  expect(requests.filter((r) => r.method === "PUT")).toHaveLength(1);
  await act(async () => {
    root.update(
      createElement(
        QueryClientProvider,
        { client },
        createElement(ReplyBox, {
          ...replyProps,
          answer: { ...answer, extId: "B", container: "thread-2" },
        }),
      ),
    );
  });
  await settle();
  await act(async () => {
    preflight.resolve(json({ ...saved, messageId: "m2" }));
  });
  await settle();
  expect(fields().draft.body).toBe("OLD SAVED WORDS");
  expect(requests.filter((r) => r.body.confirm === true || r.method === "DELETE")).toEqual([]);
});

describe("immutable review requests", () => {
  it("rejects a deferred reply preview after retargeting, including A -> B -> A", async () => {
    const preview = deferred<Response>();
    const normal = handle;
    handle = (req) => (new URL(req.url).pathname === "/v1/send" ? preview.promise : normal(req));
    await mount();
    await typeReply("My words");
    await click("preview");
    for (const extId of ["B", "A"]) {
      await act(async () => {
        root.update(
          createElement(
            QueryClientProvider,
            { client },
            createElement(ReplyBox, {
              ...replyProps,
              answer: { ...answer, extId },
            }),
          ),
        );
      });
      await settle();
    }
    await act(async () => {
      preview.resolve(json({ accountId: "acct", entry: "A", to: "old@example.test" }));
    });
    await settle();
    expect(button("send this reply")).toBeUndefined();
    expect(fields().draft.body).toBe("My words");
    expect(requests.filter((r) => r.body.confirm === true)).toEqual([]);
  });

  it("keeps draft cleanup attached to the sent owner if the target changes during send", async () => {
    const sent = deferred<Response>();
    const normal = handle;
    handle = async (req) => {
      const path = new URL(req.url).pathname;
      if (path === "/v1/drafts" && req.method === "GET") return json([saved]);
      if (path === "/v1/send" && (await req.clone().json()).confirm) return sent.promise;
      return normal(req);
    };
    await mount();
    await click("preview");
    await click("send this reply");
    await act(async () => {
      root.update(
        createElement(
          QueryClientProvider,
          { client },
          createElement(ReplyBox, {
            ...replyProps,
            answer: { ...answer, extId: "B", container: "thread-2" },
          }),
        ),
      );
    });
    await settle();
    await act(async () => {
      sent.resolve(json({ sent: true }));
    });
    await settle();
    expect(fields().draft.body).toBe("OLD SAVED WORDS");
    expect(requests.filter((r) => r.method === "DELETE").map((r) => r.path)).toEqual([
      "/v1/drafts/d1",
    ]);
    expect(requests.find((r) => r.body.confirm === true)?.body.entry).toBe("A");
  });

  it("invalidates an already displayed reply plan when reply-all changes", async () => {
    await mount();
    await typeReply("My words");
    await click("preview");
    await act(async () => {
      root.update(
        createElement(
          QueryClientProvider,
          { client },
          createElement(ReplyBox, {
            ...replyProps,
            all: false,
          }),
        ),
      );
    });
    await settle();
    expect(button("send this reply").props.disabled).toBe(true);
    // Even a direct stale handler invocation cannot send.
    await act(async () => {
      button("send this reply").props.onClick();
    });
    expect(requests.filter((r) => r.body.confirm === true)).toEqual([]);
  });

  it("can confirm Unicode To/Cc names despite canonical RFC 2047 preview headers", async () => {
    const normal = handle;
    handle = (req) =>
      new URL(req.url).pathname === "/v1/compose"
        ? Promise.resolve(
            json({
              accountId: "acct",
              to: "=?utf-8?q?Jos=C3=A9?= <jose@example.test>",
              cc: "=?utf-8?q?Zo=C3=AB?= <zoe@example.test>",
              subject: "Hello",
              body: "Words",
            }),
          )
        : normal(req);
    await mount(false);
    await act(async () => {
      form().onDraft({
        to: [{ name: "José", address: "jose@example.test" }],
        cc: [{ name: "Zoë", address: "zoe@example.test" }],
        subject: "Hello",
        body: "Words",
      });
    });
    await act(async () => {
      form().onSubmit({ preventDefault() {} } as Parameters<
        ComponentProps<typeof ComposeForm>["onSubmit"]
      >[0]);
    });
    await settle();
    await click("Confirm and send");
    const sent = requests.find((r) => r.body.confirm === true)!;
    expect(sent.body.to).toEqual(["José <jose@example.test>"]);
    expect(sent.body.cc).toEqual(["Zoë <zoe@example.test>"]);
  });

  it("rejects a deferred compose preview when the sender changes", async () => {
    const preview = deferred<Response>();
    const normal = handle;
    handle = (req) => (new URL(req.url).pathname === "/v1/compose" ? preview.promise : normal(req));
    await mount(false);
    await act(async () => {
      form().onDraft({ to: [{ address: "to@example.test" }], body: "Words" });
    });
    await act(async () => {
      form().onSubmit({ preventDefault() {} } as Parameters<
        ComponentProps<typeof ComposeForm>["onSubmit"]
      >[0]);
    });
    await act(async () => {
      form().onAccount("other-account");
    });
    await act(async () => {
      preview.resolve(json({ accountId: "acct", to: "to@example.test", body: "Words" }));
    });
    await settle();
    expect(button("Confirm and send")).toBeUndefined();
  });
});
