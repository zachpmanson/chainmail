// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { makeQueryClient } from "../src/lib/queryClient";
import { clearToasts } from "../src/lib/toasts";
import { createChainmailRouter } from "../src/router";

/**
 * Answering a message from the reading pane: the box, the two presses, and the
 * words a reader is given between them.
 *
 * What the server does with a send is its own tests' business (cmd/server's
 * send_test.go); what is asserted here is the client's half — that the box names
 * the message it will answer, that a message the mailbox does not hold is not
 * offered as one, that nothing is written to the mailbox until the second press,
 * that the second press sends the body the reader was shown, and that a reader is
 * told in words when the host cannot answer mail at all. Everything below is
 * invented; this corpus holds real correspondence.
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

const ROOT = "mail:<loom-cutover-1@example.fed>";
const QUOTED = "quote:deadbeef";

/** Where a message lives in the mailbox: the corpus answers a mailbox id in a
 *  permalink, which is how every mailbox-backed thing in the pane is conditional
 *  on it (see lib/sources' gmailIdOf). Absent is a message the corpus holds and
 *  the mailbox does not — a line recovered from inside somebody's quote. */
const mailbox = (id: string) => `https://mail.google.com/mail/u/0/#all/${id}`;

const entry = (over: Record<string, unknown>) => ({
  extId: ROOT,
  source: "mail",
  quoted: false,
  ts: "2026-03-11T17:40:00+11:00",
  author: "Bo Halvorsen",
  subject: "Loom cutover schedule",
  body: "Roof access is fine from the 14th.",
  html: "<p>Roof access is fine from the 14th.</p>",
  tz: "AEDT",
  tzOffsetMinutes: 660,
  org: "Loomworks",
  participants: [{ personId: 1, name: "Bo Halvorsen", role: "from" }],
  attachments: [],
  permalink: mailbox("g-1"),
  ...over,
});

/** One thread as /v1/chains answers it: a message the mailbox holds, and — for
 *  the tests that need one — a newer line recovered from a quote, which is the
 *  entry nothing can be threaded onto. */
const chainBody = (entries: unknown[]) => ({ rootExtId: ROOT, entries });

const threaded = [
  entry({ extId: ROOT }),
  entry({
    extId: QUOTED,
    author: "Cy Devlin",
    ts: "2026-03-12T09:00:00Z",
    quoted: true,
    body: "and the fitters can come on the 14th",
    permalink: undefined,
  }),
];

/** What the server answers a send with: the recipient and subject its own headers
 *  give, and the whole body with the quote in it. The same three fields for the
 *  preview and the send, which is what lets a client show one and send the other. */
const replyPlan = (sent: boolean) => ({
  entry: ROOT,
  to: "Bo Halvorsen <bo@fjordline.example>",
  cc: "Cy Okafor <cy@loomworks.example>, carl@example.net",
  subject: "Re: Loom cutover schedule",
  body:
    "The 14th works.\n\n" +
    "On Wed 11 Mar 2026 17:40 AEST, Bo Halvorsen <bo@fjordline.example> wrote:\n" +
    "> Roof access is fine from the 14th.\n",
  sent,
  ...(sent ? { gmailId: "g-9" } : {}),
});

/** The page's list, and the endpoints the pane reaches for around it. */
const server = (chain: () => Response, send?: Handler): Handler => (c) => {
  const p = pathOf(c);
  if (p === "/v1/search") {
    return json(200, {
      mode: "lexical",
      chains: [
        {
          rootExtId: ROOT,
          subject: "Loom cutover schedule",
          first: "2026-03-02T09:15:00Z",
          last: "2026-03-12T09:00:00Z",
          sources: ["mail"],
          entries: 2,
          matched: 2,
          people: 2,
          attachments: 0,
          score: 0.01,
          unread: 0,
          best: [entry({ extId: ROOT })],
        },
      ],
    });
  }
  if (p.startsWith("/v1/chains/")) return chain();
  if (p === "/v1/send") return send ? send(c) : json(500, { error: "no send handler" });
  if (p === "/v1/labels") return json(200, { labels: [{ name: "INBOX", messages: 5 }] });
  if (p === "/v1/stats") return json(200, {});
  if (p === "/v1/settings") return json(200, {});
  if (p === "/auth/status") return json(200, { signed_in: true });
  return json(500, { error: `unexpected call to ${c.method} ${p}` });
};

const sends = () => calls.filter((c) => c.method === "POST" && pathOf(c) === "/v1/send");
const chains = () => calls.filter((c) => pathOf(c).startsWith("/v1/chains/"));
const sent = (i: number) => JSON.parse(sends()[i]!.body!) as Record<string, unknown>;

beforeEach(() => {
  calls = [];
  // The notifications are one store shared by every writer in the app, so a test
  // starts with none standing — a send's own account is asserted below, and it
  // must be that send's rather than a neighbour's.
  clearToasts();
  (globalThis as { IntersectionObserver?: unknown }).IntersectionObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
    takeRecords() {
      return [];
    }
  };
  handler = () => json(500, { error: "no handler installed" });
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
  clearToasts();
});

async function mountApp() {
  const router = createChainmailRouter(["/"]);
  await router.load();
  render(
    <QueryClientProvider client={makeQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

const pane = () => document.querySelector(".ibread") as HTMLElement;
const box = () => pane().querySelector(".replybox") as HTMLElement | null;
const field = () => screen.getByLabelText("Your reply") as HTMLTextAreaElement;
const press = (name: string) => fireEvent.click(within(box()!).getByRole("button", { name }));

async function openRow() {
  await waitFor(() => expect(document.querySelectorAll(".ibrow button").length).toBeGreaterThan(0));
  fireEvent.click(document.querySelectorAll(".ibrow button")[0] as HTMLElement);
}

/** Open the thread the pane replies from: the pane starts empty, and a reply box
 *  is a thing that appears once there is a message to answer. */
async function openThread() {
  await openRow();
  await waitFor(() => expect(box()).toBeTruthy());
}

/** Type a reply and take it as far as the first press, against a server that
 *  prepares the reply with the plan above unless a test says otherwise.
 *
 *  The answer is a factory rather than a response, because a body can be read
 *  once: the send is a second call to the same endpoint, and a test that handed
 *  back the object it had already answered with would fail on an empty body
 *  rather than on anything the component did. */
async function write(text: string, answer: () => Response = () => json(200, replyPlan(false))) {
  handler = server(
    () => json(200, chainBody(threaded)),
    () => answer(),
  );
  await mountApp();
  await openThread();
  fireEvent.change(field(), { target: { value: text } });
  press("preview");
}

describe("answering a message from the pane", () => {
  it("answers the newest message the mailbox holds, not the newest said", async () => {
    handler = server(() => json(200, chainBody(threaded)));
    await mountApp();
    await openThread();

    // The thread's newest entry is a line recovered from a quote, so there is no
    // mailbox message to thread an answer onto. The box says which message it will
    // answer rather than leaving the reader to guess, and it is not that one.
    const to = box()!.querySelector(".replyto")!.textContent!;
    expect(to).toContain("Bo Halvorsen");
    expect(to).not.toContain("Cy Devlin");
    expect(to).toContain("Wed, 11 Mar 2026 17:40");
    // And it says the reply reaches the message's whole audience, which is what
    // the server is about to do: the box names the message it answers, and "who
    // else" is not something it can know before the plan comes back.
    expect(to).toContain("everyone else");
    // The words are the bubble's own, which is the point of the pairing: the head
    // above says when the message arrived, and this line must not say otherwise.
    expect(sends()).toHaveLength(0);
  });

  it("offers no box at all when nothing in the thread is in the mailbox", async () => {
    const only = [entry({ extId: QUOTED, quoted: true, permalink: undefined })];
    handler = server(() => json(200, chainBody(only)));
    await mountApp();
    await openRow();

    // A composer here could only offer a press the server must refuse.
    await waitFor(() => expect(pane().querySelector(".msg")).toBeTruthy());
    expect(box()).toBeNull();
  });

  it("holds its buttons at the right end, with the press that sends last", async () => {
    handler = server(
      () => json(200, chainBody(threaded)),
      () => json(200, replyPlan(false)),
    );
    await mountApp();
    await openThread();
    await waitFor(() => expect(box()!.querySelector(".replyacts")).toBeTruthy());

    // Which end the row sits at is the stylesheet's answer (`.replyacts`), and
    // jsdom applies no stylesheets — so what is pinned here is what the
    // stylesheet right-aligns, and in what order: the last button in a
    // right-aligned row is the one nearest the reader's cursor, and the press
    // that sends is the one the plan exists to be checked before.
    const type = box()!.querySelector(".replyacts")!;
    expect(within(type as HTMLElement).getAllByRole("button").map((b) => b.textContent)).toEqual([
      "preview",
    ]);

    fireEvent.change(field(), { target: { value: "The 14th works." } });
    press("preview");
    await waitFor(() => expect(box()!.querySelector(".replyplan")).toBeTruthy());
    const plan = box()!.querySelector(".replyacts")!;
    expect(within(plan as HTMLElement).getAllByRole("button").map((b) => b.textContent)).toEqual([
      "keep editing",
      "send this reply",
    ]);
  });

  it("sends nothing on the first press, and shows what the second would send", async () => {
    await write("The 14th works.");

    // One call, and it is a preview: the reply was prepared and answered with, and
    // no mailbox was written to.
    await waitFor(() => expect(sends()).toHaveLength(1));
    expect(sent(0)).toEqual({ entry: ROOT, body: "The 14th works.", all: true, confirm: false });

    // What the reader is shown is the whole message as it will go, and the first
    // thing it says is that nothing has left yet — a preview that did not say so
    // would be a screen where the reader cannot tell whether they had sent.
    const shown = box()!;
    expect(shown.querySelector(".replynote")!.textContent).toContain("Nothing has been sent yet.");
    expect(shown.querySelector(".replynote")!.textContent).toContain(
      "Bo Halvorsen <bo@fjordline.example>",
    );
    // Everyone else is named too, because a reply-all's plan is only checkable if
    // the reader can see who else is on it — the one thing the preview exists for.
    expect(shown.querySelector(".replynote")!.textContent).toContain("cc");
    expect(shown.querySelector(".replynote")!.textContent).toContain(
      "Cy Okafor <cy@loomworks.example>, carl@example.net",
    );
    expect(shown.querySelector(".replynote")!.textContent).toContain("Re: Loom cutover schedule");
    // The body, quote and all, as the exact lines that will be sent.
    const text = shown.querySelector(".replytext")!.textContent!;
    expect(text).toContain("The 14th works.");
    expect(text).toContain("> Roof access is fine from the 14th.");
    expect(text).toContain("wrote:");
    // The field is gone while the plan is up: a reply being sent is not a reply
    // being edited, and the text under the reader's cursor would be the one thing
    // they cannot check.
    expect(screen.queryByLabelText("Your reply")).toBeNull();
  });

  it("answers the sender alone when the reply-all tick is cleared", async () => {
    handler = server(() => json(200, chainBody(threaded)));
    await mountApp();
    await openThread();
    await waitFor(() => expect(box()).toBeTruthy());

    // The tick is on by default, and the line above the box says so: who the reply
    // is going to is stated before the reader presses anything, so the one thing
    // about the audience they get to decide is also the one thing they are told.
    const tick = within(box()!).getByRole("checkbox", { name: /reply all/ }) as HTMLInputElement;
    expect(tick.checked).toBe(true);
    expect(box()!.querySelector(".replyto")!.textContent).toContain("everyone else");
    // It is the left end of the row whose buttons are at the right: the choice
    // under the field, the press past it.
    expect(box()!.querySelector(".replyacts .replyall")).toBeTruthy();

    fireEvent.click(tick);
    expect(tick.checked).toBe(false);
    const said = box()!.querySelector(".replyto")!.textContent!;
    expect(said).toContain("nobody else");
    expect(said).not.toContain("everyone else");

    fireEvent.change(field(), { target: { value: "The 14th works." } });
    press("preview");

    // And the setting is what the server is asked for rather than only what the
    // box says: answering one person when twelve are on the message is the thing
    // this control exists to make possible.
    await waitFor(() => expect(sends()).toHaveLength(1));
    expect(sent(0)).toEqual({ entry: ROOT, body: "The 14th works.", all: false, confirm: false });
  });

  it("names no cc when the reply has nobody else on it", async () => {
    // A message the reader was the only recipient of answers with no cc at all
    // (see the contract's SendResponse), and the plan must not invent an empty
    // recipient list to draw beside the one it does have.
    await write("The 14th works.", () => json(200, { ...replyPlan(false), cc: undefined }));

    await waitFor(() => expect(box()!.querySelector(".replynote")).toBeTruthy());
    const note = box()!.querySelector(".replynote")!.textContent!;
    expect(note).toContain("Bo Halvorsen <bo@fjordline.example>");
    expect(note).not.toContain("cc");
  });

  it("sends the body the reader was shown, and re-reads the trail it lands in", async () => {
    await write("The 14th works.");
    await waitFor(() => expect(sends()).toHaveLength(1));
    const before = chains().length;

    fireEvent.click(within(box()!).getByRole("button", { name: "send this reply" }));

    await waitFor(() => expect(sends()).toHaveLength(2));
    expect(sent(1)).toEqual({ entry: ROOT, body: "The 14th works.", all: true, confirm: true });
    // The reply is filed into the corpus by the server, so the trail the reader is
    // looking at is re-read rather than guessed at: the answer appears in it as the
    // mailbox's own copy of the message.
    await waitFor(() => expect(chains().length).toBeGreaterThan(before));
    // And the box is empty again, with the account of what happened in the words
    // the surface uses for it — in the shell's corner, where the app keeps work
    // that is over (see Toasts), not in a row of the trail being read.
    await waitFor(() =>
      expect(document.querySelector(".toast")!.textContent).toContain("Answered Bo Halvorsen"),
    );
    expect(box()!.querySelector(".pullnote")).toBeNull();
    expect(box()!.querySelector(".replytext")).toBeNull();
    expect(field().value).toBe("");
  });

  it("says it once, and says the last one", async () => {
    await write("The 14th works.");
    await waitFor(() => expect(box()!.querySelector(".replytext")).toBeTruthy());
    press("send this reply");
    await waitFor(() => expect(document.querySelectorAll(".toast")).toHaveLength(1));

    // A second reply is a second send, and the corner holds the account of the
    // last thing this box did rather than one sentence per reply — a reply cannot
    // be recalled, so two accounts of two of them is a reader counting.
    fireEvent.change(field(), { target: { value: "And the fitters?" } });
    press("preview");
    await waitFor(() => expect(box()!.querySelector(".replytext")).toBeTruthy());
    press("send this reply");

    await waitFor(() => expect(sends()).toHaveLength(4));
    expect(document.querySelectorAll(".toast")).toHaveLength(1);
    expect(document.querySelector(".toast")!.textContent).toContain("Answered Bo Halvorsen");
  });

  it("tells the reader when this host cannot answer mail at all", async () => {
    await write("The 14th works.", () =>
      json(403, { error: "answering mail is disabled: start the server with -send-mail" }),
    );

    const fail = await waitFor(() => {
      const el = box()!.querySelector(".selfail");
      expect(el).toBeTruthy();
      return el!;
    });
    expect(fail.textContent).toContain("-send-mail");
    // The reader's own words are still on the pane: a refusal is not a reason to
    // lose what they wrote.
    expect(field().value).toBe("The 14th works.");
  });

  it("will not preview an empty reply", async () => {
    handler = server(() => json(200, chainBody(threaded)));
    await mountApp();
    await openThread();

    const preview = within(box()!).getByRole("button", { name: "preview" });
    expect(preview.hasAttribute("disabled")).toBe(true);
    // Whitespace is not something to say.
    fireEvent.change(field(), { target: { value: "  \n " } });
    expect(preview.hasAttribute("disabled")).toBe(true);
    fireEvent.change(field(), { target: { value: "ok" } });
    expect(preview.hasAttribute("disabled")).toBe(false);
    expect(sends()).toHaveLength(0);
  });
});
