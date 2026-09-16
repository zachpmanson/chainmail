// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { makeQueryClient } from "../src/lib/queryClient";
import { createChainmailRouter } from "../src/router";

/**
 * Organisations: the colour a bubble is drawn in, and the rule behind it.
 *
 * Two surfaces are asserted here because they are one claim. The Ops screen is
 * where the reader answers "whose mail is this", and the reading pane is where
 * the answer shows — the same resolver feeds both, so a rule that agrees with
 * itself on the Ops screen and disagrees in the pane is the defect this covers.
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

/** The domains the corpus holds, as GET /v1/ops/orgs answers them: counted from
 *  the mail's own From header, with `stored` telling the reader's answer from the
 *  guess read off the domain's name. */
const DOMAINS = () => [
  {
    domain: "loomworks.example",
    messages: 3,
    people: 1,
    org: "Loomworks",
    stored: false,
    guess: "Loomworks",
  },
  {
    domain: "fjordline.example",
    messages: 1,
    people: 1,
    org: "Fjordline",
    stored: false,
    guess: "Fjordline",
  },
];

/** The consequence the resolver reports for the rule in each test — the numbers
 *  are the server's, and the screen's job is to say them rather than invent them. */
let shift = { messages: 0, people: 0, ambiguous: 0 };

/** The one chain the pane is pointed at, with the organisations the resolver put
 *  on each entry — the field this whole issue exists to carry. */
const CHAIN_ENTRIES = [
  { extId: "mail:<a@example.fed>", ts: "2026-03-02T09:15:00Z", html: "<p>Ada's</p>", author: "Ada Byron", org: "Loomworks" },
  { extId: "mail:<b@example.fed>", ts: "2026-03-03T09:15:00Z", html: "<p>Bo's</p>", author: "Bo Halvorsen", org: "Fjordline" },
  { extId: "mail:<c@example.fed>", ts: "2026-03-04T09:15:00Z", html: "<p>Ada again</p>", author: "Ada Byron", org: "Loomworks" },
  // A sender whose organisation nothing established: no colour is claimed, and
  // the unknown slot is what the page draws for them too.
  { extId: "mail:<d@example.fed>", ts: "2026-03-05T09:15:00Z", html: "<p>Nobody's</p>", author: "Cy Okafor" },
];

const opsHandler: Handler = (c) => {
  const p = pathOf(c);
  if (p === "/v1/ops/plan") return json(200, { people: 2, merges: [], refusals: [], candidates: [], twinsDeclined: [], trail: [] });
  if (p === "/v1/ops/orgs" && c.method === "GET") return json(200, { domains: DOMAINS() });
  if (p === "/v1/ops/orgs/preview") return json(200, { domain: "x.example", ...shift });
  if (p === "/v1/ops/orgs" && c.method === "POST") return json(200, { domains: DOMAINS() });
  if (p === "/v1/settings") return json(200, {});
  if (p === "/auth/status") return json(200, { signed_in: true });
  return json(500, { error: `unexpected call to ${c.method} ${p}` });
};

const paneHandler: Handler = (c) => {
  const p = pathOf(c);
  if (p === "/v1/search") return json(200, { mode: "lexical", chains: [] });
  if (p.startsWith("/v1/chains/")) {
    return json(200, { rootExtId: "mail:<a@example.fed>", entries: CHAIN_ENTRIES });
  }
  return opsHandler(c);
};

beforeEach(() => {
  calls = [];
  shift = { messages: 0, people: 0, ambiguous: 0 };
  handler = () => json(500, { error: "no handler installed" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const req =
      input instanceof Request ? input : new Request(new URL(String(input), location.href), init);
    const call: Call = { url: req.url, method: req.method };
    if (req.method !== "GET") call.body = await req.text();
    // The nav's deploy stamp asks /v1/version on every route; no test here is
    // about it, and answering it keeps it out of the recorded calls.
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

/** One domain's row. Every row offers a save, so an assertion about a row's own
 *  controls has to be scoped to that row rather than to the screen. */
const row = (domain: string) => screen.getByText(domain).closest(".oprow") as HTMLElement;

describe("the organisations screen", () => {
  it("lists each domain with its mail and whether the grouping is the reader's", async () => {
    handler = opsHandler;
    await mountApp("/ops");

    expect(await screen.findByText("loomworks.example")).toBeTruthy();
    expect(screen.getByText("fjordline.example")).toBeTruthy();
    // The count is the reason a rule is worth writing about this domain and not
    // another, so it is on the row rather than in a tooltip.
    expect(screen.getByText(/3 messages from 1 sender/)).toBeTruthy();
    expect(screen.getByText(/1 message from 1 sender/)).toBeTruthy();
    // And nothing here is stored yet: the labels on screen are the corpus's
    // guesses, which the reader is looking at in order to accept or overrule.
    expect(screen.getAllByText("guessed")).toHaveLength(2);
  });

  it("counts the change before the rule is written, and writes it only after", async () => {
    handler = opsHandler;
    shift = { messages: 1, people: 1, ambiguous: 0 };
    await mountApp("/ops");
    await screen.findByText("fjordline.example");

    const field = screen.getByLabelText("Organisation for fjordline.example") as HTMLInputElement;
    fireEvent.change(field, { target: { value: "Loomworks" } });
    click(within(row("fjordline.example")).getByRole("button", { name: "save" }));

    // The preview is the model of the change: its answer is what the reader is
    // asked to agree to, so it must be counted before anything is written.
    await waitFor(() => expect(callsTo("POST", "/v1/ops/orgs/preview")).toHaveLength(1));
    expect(callsTo("POST", "/v1/ops/orgs/preview")[0]!.body).toBe(
      JSON.stringify({ domain: "fjordline.example", org: "Loomworks" }),
    );
    expect(callsTo("POST", "/v1/ops/orgs")).toHaveLength(0);
    expect(await screen.findByText(/1 message from 1 sender would be drawn differently/)).toBeTruthy();

    click(screen.getByRole("button", { name: "save this grouping" }));
    await waitFor(() => expect(callsTo("POST", "/v1/ops/orgs")).toHaveLength(1));
    expect(callsTo("POST", "/v1/ops/orgs")[0]!.body).toBe(
      JSON.stringify({ domain: "fjordline.example", org: "Loomworks" }),
    );
    // And the list is re-read afterwards, so the row shows what was stored rather
    // than what was asked for.
    await waitFor(() => expect(callsTo("GET", "/v1/ops/orgs").length).toBeGreaterThan(1));
  });

  it("cancels without writing", async () => {
    handler = opsHandler;
    await mountApp("/ops");
    await screen.findByText("fjordline.example");
    fireEvent.change(screen.getByLabelText("Organisation for fjordline.example"), {
      target: { value: "Loomworks" },
    });
    click(within(row("fjordline.example")).getByRole("button", { name: "save" }));
    await screen.findByText(/would be drawn differently|No message changes colour/);
    click(screen.getByRole("button", { name: "cancel" }));
    expect(callsTo("POST", "/v1/ops/orgs")).toHaveLength(0);
  });

  it("drops a rule by sending no organisation at all, not an empty one", async () => {
    // A rule the reader already made, which is the only state that offers the
    // third act: putting the domain back to its own name.
    handler = (c) =>
      pathOf(c) === "/v1/ops/orgs" && c.method === "GET"
        ? json(200, { domains: [{ ...DOMAINS()[0]!, org: "The Loom", stored: true }] })
        : opsHandler(c);
    await mountApp("/ops");
    expect(await screen.findByText(/cleared, it is read as Loomworks/)).toBeTruthy();
    click(within(row("loomworks.example")).getByRole("button", { name: "clear" }));
    await waitFor(() => expect(callsTo("POST", "/v1/ops/orgs/preview")).toHaveLength(1));
    // Absent is the answer "there should be no rule", and an empty string would
    // be a different one — "this domain is nobody's" — so the key is not sent.
    expect(callsTo("POST", "/v1/ops/orgs/preview")[0]!.body).toBe(
      JSON.stringify({ domain: "loomworks.example" }),
    );
    expect(await screen.findByText(/goes back to being read from its own name/)).toBeTruthy();
    click(screen.getByRole("button", { name: "drop the rule" }));
    await waitFor(() => expect(callsTo("POST", "/v1/ops/orgs")).toHaveLength(1));
    expect(callsTo("POST", "/v1/ops/orgs")[0]!.body).toBe(
      JSON.stringify({ domain: "loomworks.example" }),
    );
  });
});

describe("the reading pane's colours", () => {  it("draws each sender on their organisation's slot, in the chain's own order", async () => {
    handler = paneHandler;
    await mountApp(`/?open=${encodeURIComponent("mail:<a@example.fed>")}`);

    await waitFor(() => expect(document.querySelectorAll(".ibread .msg").length).toBe(4));
    const msgs = [...document.querySelectorAll(".ibread .msg")];
    // First appearance order, so the first sender's organisation is o1 and the
    // second's o2 — and a repeat keeps the slot the first one took, which is what
    // makes one person one colour on a page that also draws them.
    expect(msgs.map((m) => m.className.split(" ")[1])).toEqual(["o1", "o2", "o1", "o5"]);
    // The avatar is coloured from the same slot: a bubble and the picture beside
    // it cannot disagree.
    const avs = [...document.querySelectorAll(".ibread .msg .av")];
    expect(avs.map((a) => a.className.split(" ")[1])).toEqual(["o1", "o2", "o1", "o5"]);
  });
});
