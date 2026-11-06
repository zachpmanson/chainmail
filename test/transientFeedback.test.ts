import { createElement, type ComponentProps } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import OpsPeople from "../src/components/ops/OpsPeople";
import OpsOrgs from "../src/components/ops/OpsOrgs";
import OpsMerges from "../src/components/ops/OpsMerges";
import PersonRow from "../src/components/ops/PersonRow";
import RuleRow from "../src/components/ops/RuleRow";
import MergeCard from "../src/components/ops/MergeCard";
import { useMergeRun } from "../src/lib/ops/opsMerges";
import { makeQueryClient } from "../src/lib/api/queryClient";
import { clearToasts, dismissToast, useToasts, type Toast } from "../src/lib/ui/toasts";

vi.mock("../src/components/ops/PersonRow", () => ({
  default: (props: object) => createElement("person-row", props),
}));
vi.mock("../src/components/ops/RuleRow", () => ({
  default: (props: object) => createElement("rule-row", props),
}));
vi.mock("../src/components/ops/MergeCard", () => ({
  default: (props: object) => createElement("merge-card", props),
}));
vi.mock("../src/components/ops/MergePlanDetails", () => ({ default: () => null }));
vi.mock("../src/components/ops/Consequence", () => ({ default: () => null }));

let root: ReactTestRenderer;
let observer: ReactTestRenderer;
let client: QueryClient;
let handle: (request: Request) => Promise<Response>;
const pairs = [20, 30, 40].map((dropId) => ({
  keepId: 10,
  dropId,
  keepName: "Keeper",
  dropName: `Person ${dropId}`,
  applicable: true,
}));
const json = (data: unknown, status = 200) =>
  new Response(JSON.stringify(data), { status, headers: { "Content-Type": "application/json" } });
function deferred() {
  let resolve!: (value: Response) => void;
  const promise = new Promise<Response>((yes) => {
    resolve = yes;
  });
  return { promise, resolve };
}
function Snapshot() {
  return createElement("toast-model", { notifications: useToasts() });
}
const notes = (): Toast[] => observer.root.findByType("toast-model" as never).props.notifications;
const text = () =>
  root.root
    .findAll((node) => typeof node.type === "string")
    .flatMap((node) => node.children.filter((child) => typeof child === "string"))
    .join(" ");
const person = () =>
  root.root.findByType("person-row" as never).props as ComponentProps<typeof PersonRow>;
const rule = () =>
  root.root.findByType("rule-row" as never).props as ComponentProps<typeof RuleRow>;
async function settle() {
  for (let i = 0; i < 5; i++)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
}
async function mount(component: typeof OpsPeople | typeof OpsOrgs | typeof OpsMerges) {
  await act(async () => {
    root = create(createElement(QueryClientProvider, { client }, createElement(component)));
  });
  await settle();
}
async function click(label: string) {
  const button = root.root
    .findAllByType("button")
    .find((node) => node.children.join("") === label)!;
  expect(button.props.disabled).not.toBe(true);
  await act(async () => {
    void button.props.onClick();
  });
  await settle();
}
beforeEach(async () => {
  vi.useFakeTimers();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  clearToasts();
  client = makeQueryClient();
  handle = async (req) => {
    const path = new URL(req.url).pathname;
    if (req.method !== "GET") return json({ messages: 1, people: 1, ambiguous: 0 });
    if (path === "/v1/people")
      return json({ people: [{ personId: 1, displayName: "Original", identities: [] }] });
    if (path === "/v1/ops/orgs")
      return json({
        domains: [{ domain: "example.test", org: "Old org", messages: 1, people: 1, stored: true }],
      });
    if (path === "/v1/ops/plan") return json({ merges: pairs });
    return json({});
  };
  vi.stubGlobal("fetch", (req: Request) => handle(req));
  await act(async () => {
    observer = create(createElement(Snapshot));
  });
});
afterEach(async () => {
  await act(async () => {
    root?.unmount();
    observer.unmount();
  });
  clearToasts();
  client.clear();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it("toasts each actual person edit success before readback, without a layout receipt or false failure success", async () => {
  await mount(OpsPeople);
  const pending = deferred();
  const readback = deferred();
  const normal = handle;
  handle = (req) =>
    req.method === "POST"
      ? pending.promise
      : req.url.endsWith("/v1/people")
        ? readback.promise
        : normal(req);
  await act(async () => {
    person().onEdit({ displayName: "Updated" });
  });
  await settle();
  expect(notes()).toEqual([]);
  await act(async () => {
    pending.resolve(json({}));
  });
  await settle();
  expect(notes()).toEqual([{ id: expect.any(Number), text: "#1 is called Updated", kind: "note" }]);
  expect(text()).not.toContain("#1 is called Updated");
  expect(text()).not.toContain("list below is the current one");
  const first = notes()[0]!.id;
  await act(async () => {
    readback.resolve(json({ people: [{ personId: 1, displayName: "Original", identities: [] }] }));
  });
  await settle();
  handle = normal;
  await act(async () => {
    person().onEdit({ displayName: "Updated" });
  });
  await settle();
  expect(notes()).toHaveLength(1);
  expect(notes()[0]!.id).toBeGreaterThan(first);
  await act(async () => {
    dismissToast(notes()[0]!.id);
  });
  handle = (req) =>
    req.method === "POST" ? Promise.resolve(json({ error: "Edit refused" }, 409)) : normal(req);
  await act(async () => {
    person().onEdit({ displayName: "Updated" });
  });
  await settle();
  expect(notes()).toEqual([]);
  expect(text()).toContain("Edit refused");
});

it.each(["New org", "", null])(
  "toasts rule mutations only after success, not preview or refusal (org: %s)",
  async (org) => {
    await mount(OpsOrgs);
    const normal = handle;
    const pending = deferred();
    handle = (req) =>
      req.method === "POST" && new URL(req.url).pathname === "/v1/ops/orgs"
        ? pending.promise
        : normal(req);
    await act(async () => {
      rule().onPick({ domain: "example.test", org });
    });
    await settle();
    expect(notes()).toEqual([]);
    const label = org === null ? "drop the rule" : "save this grouping";
    await click(label);
    expect(notes()).toEqual([]);
    await act(async () => {
      pending.resolve(json({}));
    });
    await settle();
    const receipt =
      org === null
        ? "dropped the rule about example.test"
        : org === ""
          ? "example.test is not an organisation"
          : "example.test is drawn as New org";
    expect(notes()).toEqual([{ id: expect.any(Number), text: receipt, kind: "note" }]);
    expect(text()).not.toContain(receipt);
    expect(text()).not.toContain("list below is the current one");
    const first = notes()[0]!.id;
    handle = normal;
    await act(async () => {
      rule().onPick({ domain: "example.test", org });
    });
    await settle();
    await click(label);
    expect(notes()).toHaveLength(1);
    expect(notes()[0]!.id).toBeGreaterThan(first);
    await act(async () => {
      dismissToast(notes()[0]!.id);
    });
    handle = (req) =>
      req.method === "POST" && new URL(req.url).pathname === "/v1/ops/orgs"
        ? Promise.resolve(json({ error: "Rule refused" }, 409))
        : normal(req);
    await act(async () => {
      rule().onPick({ domain: "example.test", org });
    });
    await settle();
    await click(label);
    expect(notes()).toEqual([]);
    expect(text()).toContain("Rule refused");
  },
);

it("toasts actual partial merge completions, retaining inline counts/errors and progress without stale receipts", async () => {
  await mount(OpsMerges);
  const first = deferred();
  const normal = handle;
  handle = async (req) => {
    if (req.method !== "POST") return normal(req);
    const body = await req.clone().json();
    return body.dropId === 20 ? first.promise : json({ error: "Plan changed" }, 409);
  };
  await act(async () => {
    for (const node of root.root.findAllByType("merge-card" as never).slice(0, 2))
      (node.props as ComponentProps<typeof MergeCard>).onSelect(true);
  });
  await click("merge 2 selected");
  await click("merge these 2 pairs");
  expect(text()).toContain("merging 1 of 2");
  expect(notes()).toEqual([]);
  await act(async () => {
    first.resolve(json({}));
  });
  await settle();
  expect(notes()).toEqual([{ id: expect.any(Number), text: "merged 1 pair", kind: "note" }]);
  expect(text()).toContain("1 of 2 merged, then folding #30 into #10 was refused: Plan changed");
  expect(text()).not.toContain("plan below is the current one");
  const id = notes()[0]!.id;
  handle = normal;
  await act(async () => {
    (
      root.root.findAllByType("merge-card" as never)[0]!.props as ComponentProps<typeof MergeCard>
    ).onSelect(true);
  });
  await click("merge 1 selected");
  await click("merge this pair");
  expect(notes()).toHaveLength(1);
  expect(notes()[0]!.id).toBeGreaterThan(id);
  await act(async () => {
    dismissToast(notes()[0]!.id);
  });
  handle = (req) =>
    req.method === "POST" ? Promise.resolve(json({ error: "Refused again" }, 409)) : normal(req);
  await act(async () => {
    (
      root.root.findAllByType("merge-card" as never)[0]!.props as ComponentProps<typeof MergeCard>
    ).onSelect(true);
  });
  await click("merge 1 selected");
  await click("merge this pair");
  expect(notes()).toEqual([]);
  expect(text()).toContain("0 of 1 merged");
});

it("does not toast an empty merge run", async () => {
  let run!: ReturnType<typeof useMergeRun>;
  function Harness() {
    run = useMergeRun([]);
    return null;
  }
  await act(async () => {
    root = create(createElement(QueryClientProvider, { client }, createElement(Harness)));
  });
  await act(async () => {
    await run.run();
  });
  await settle();
  expect(notes()).toEqual([]);
});
