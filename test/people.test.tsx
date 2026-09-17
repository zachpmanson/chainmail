// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { makeQueryClient } from "../src/lib/queryClient";
import { createChainmailRouter } from "../src/router";

/**
 * The people editor: who the corpus knows, what they are called, and which
 * addresses resolve to them.
 *
 * Two claims are asserted here, and they are one claim. The screen is where a
 * reader corrects a person, and the rest of the client — the list, a thread, the
 * search field's person box — reads those corrections, because they all resolve
 * through the same rows. The second claim is what the screen refuses: an address
 * another person already answers to is a merge, and a merge is made on the plan
 * with the evidence in front of it, so the refusal has to arrive as one.
 *
 * Everything below is invented; this corpus holds real correspondence.
 */

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

interface Call {
  url: string;
  method: string;
  body?: string;
}

let calls: Call[];
type Handler = (call: Call) => Promise<Response> | Response;
let handler: Handler;

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });

const pathOf = (c: Call) => new URL(c.url).pathname;
const callsTo = (method: string, path: string) =>
  calls.filter((c) => c.method === method && pathOf(c) === path);

/** The fixture corpus's cast, as GET /v1/people answers it: most involved first,
 *  with the identities the corpus resolved, including one person who is only
 *  ever a name in someone else's quoted header — which is exactly the person a
 *  reader has to be able to edit by hand. */
const ADA = {
  personId: 12,
  displayName: "Ada Okoye",
  identities: ["email:ada@loomworks.example", "display_name:ada okoye"],
  sent: 3,
  received: 4,
};
const BO = {
  personId: 19,
  displayName: "Bo Halvorsen",
  identities: ["email:bo@fjordline.example"],
  sent: 1,
  received: 3,
};
const BEN = { personId: 44, displayName: "Ben", identities: [], sent: 0, received: 1 };

/** What the server holds after a write, so a test can assert the screen shows
 *  the corpus's answer rather than what the reader asked for. */
let people = () => [ADA, BO, BEN];

/** Extra routes a single test needs — a rename that lands, a refusal. */
let onWrite: ((c: Call) => Response | undefined) | undefined;

const handler_: Handler = (c) => {
  const p = pathOf(c);
  if (p === "/v1/people" && c.method === "GET") return json(200, { people: people() });
  if (p.startsWith("/v1/people/") && c.method === "POST") {
    const mine = onWrite?.(c);
    if (mine) return mine;
    return json(200, { person: people().find((x) => `/v1/people/${x.personId}` === p)! });
  }
  // The page also draws the merge plan and the colour rules; neither is what
  // these tests are about, but both are on the same screen.
  if (p === "/v1/ops/plan") {
    return json(200, { people: 3, merges: [], refusals: [], candidates: [], twinsDeclined: [], trail: [] });
  }
  if (p === "/v1/ops/orgs") return json(200, { domains: [] });
  if (p === "/v1/settings") return json(200, {});
  if (p === "/auth/status") return json(200, { signed_in: true });
  return json(500, { error: `unexpected call to ${c.method} ${p}` });
};

beforeEach(() => {
  calls = [];
  people = () => [ADA, BO, BEN];
  onWrite = undefined;
  handler = handler_;
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const req =
      input instanceof Request ? input : new Request(new URL(String(input), location.href), init);
    const call: Call = { url: req.url, method: req.method };
    if (req.method !== "GET") call.body = await req.text();
    if (pathOf(call) === "/v1/version") return json(200, {});
    calls.push(call);
    return handler(call);
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  history.replaceState(null, "", "/");
});

async function mountApp(...initialEntries: string[]) {
  const router = createChainmailRouter(initialEntries);
  await router.load();
  render(
    <QueryClientProvider client={makeQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return router;
}

const click = (el: Element) => fireEvent.click(el);

/** One person's row. Every row carries its own fields and buttons, so anything
 *  asserted about a row has to be scoped to it — three rows of "rename" would
 *  otherwise be one assertion about the screen. */
const row = (name: string) => {
  const heading = screen.getByText(name);
  return heading.closest(".oprow") as HTMLElement;
};

describe("the people editor", () => {
  it("lists each person with their identities and counts", async () => {
    await mountApp("/ops");

    expect(await screen.findByText("Ada Okoye")).toBeTruthy();
    expect(screen.getByText("Bo Halvorsen")).toBeTruthy();
    const ada = row("Ada Okoye");
    // The identities are the reason the screen exists: they are what resolves a
    // header to this person, so they are on the row rather than behind a click.
    expect(within(ada).getByText("email:ada@loomworks.example")).toBeTruthy();
    expect(within(ada).getByText("display_name:ada okoye")).toBeTruthy();
    expect(within(ada).getByText(/sent 3 · received 4/)).toBeTruthy();
    // A person with no identity at all says so, because that is the whole of
    // what the corpus knows about them and the reason they cannot be found by
    // address anywhere else in the client.
    expect(within(row("Ben")).getByText(/only ever the name/)).toBeTruthy();
  });

  it("narrows the list to the name or the address typed", async () => {
    await mountApp("/ops");
    await screen.findByText("Ada Okoye");

    const find = screen.getByLabelText("Find a person") as HTMLInputElement;
    fireEvent.change(find, { target: { value: "bo@fjordline" } });
    // An address is the thing a reader has in hand when they are looking for a
    // person, so matching one has to work and has to be the only thing that
    // matches here: Ada's row is gone.
    expect(screen.queryByText("Ada Okoye")).toBeNull();
    expect(screen.getByText("Bo Halvorsen")).toBeTruthy();
    expect(within(row("Bo Halvorsen")).getByText("email:bo@fjordline.example")).toBeTruthy();
  });

  it("renames a person and shows what the corpus now says", async () => {
    await mountApp("/ops");
    await screen.findByText("Ada Okoye");

    const field = within(row("Ada Okoye")).getByLabelText("Name for Ada Okoye") as HTMLInputElement;
    fireEvent.change(field, { target: { value: "Ada Nwosu" } });
    // What the corpus holds once the write lands, so the assertion below is
    // about the name the reader is shown rather than the name they typed.
    people = () => [{ ...ADA, displayName: "Ada Nwosu" }, BO, BEN];
    click(within(row("Ada Okoye")).getByRole("button", { name: "rename" }));

    await waitFor(() => expect(callsTo("POST", "/v1/people/12")).toHaveLength(1));
    // Only the name: a field left out is left alone, so a rename cannot clear
    // the addresses the corpus resolved for her.
    expect(callsTo("POST", "/v1/people/12")[0]!.body).toBe(JSON.stringify({ displayName: "Ada Nwosu" }));
    // The screen shows the corpus's answer, refetched — not the box the reader
    // typed in.
    expect(await screen.findByText("Ada Nwosu")).toBeTruthy();
    await waitFor(() => expect(callsTo("GET", "/v1/people").length).toBeGreaterThan(1));
  });

  it("attaches a bare address as an email identity, and detaches one", async () => {
    await mountApp("/ops");
    await screen.findByText("Ben");

    const add = within(row("Ben")).getByLabelText("Identity to add to Ben") as HTMLInputElement;
    fireEvent.change(add, { target: { value: " ben@loomworks.example " } });
    click(within(row("Ben")).getByRole("button", { name: "attach" }));

    await waitFor(() => expect(callsTo("POST", "/v1/people/44")).toHaveLength(1));
    // A bare address is read as an email — a reader with an address in hand
    // should not have to spell its kind — and the kind is what the corpus keys
    // identities by.
    expect(callsTo("POST", "/v1/people/44")[0]!.body).toBe(
      JSON.stringify({ addIdentities: ["email:ben@loomworks.example"] }),
    );

    click(within(row("Bo Halvorsen")).getByLabelText("Detach email:bo@fjordline.example from Bo Halvorsen"));
    await waitFor(() => expect(callsTo("POST", "/v1/people/19")).toHaveLength(1));
    expect(callsTo("POST", "/v1/people/19")[0]!.body).toBe(
      JSON.stringify({ removeIdentities: ["email:bo@fjordline.example"] }),
    );
  });

  it("shows the refusal when an address belongs to somebody else", async () => {
    onWrite = (c) => {
      if (pathOf(c) === "/v1/people/19") {
        return json(409, {
          error:
            "email:ada@loomworks.example belongs to Ada Okoye (#12) — merge instead: " +
            "moving an address between people is the same act as merging them",
        });
      }
      return undefined;
    };
    await mountApp("/ops");
    await screen.findByText("Bo Halvorsen");

    const add = within(row("Bo Halvorsen")).getByLabelText(
      "Identity to add to Bo Halvorsen",
    ) as HTMLInputElement;
    fireEvent.change(add, { target: { value: "ada@loomworks.example" } });
    click(within(row("Bo Halvorsen")).getByRole("button", { name: "attach" }));

    // The refusal is the server's own sentence, because it is the part the
    // reader can act on: it names the holder and says where the merge is made.
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Ada Okoye");
    expect(alert.textContent).toContain("merge instead");
    // And nothing was written to the corpus's own list: the row still shows the
    // one address Bo answers to.
    await waitFor(() => expect(callsTo("GET", "/v1/people").length).toBeGreaterThan(1));
    expect(within(row("Bo Halvorsen")).getByText("email:bo@fjordline.example")).toBeTruthy();
  });
});
