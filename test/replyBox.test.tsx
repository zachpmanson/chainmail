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
  fromEmail: "bo@fjordline.example",
  subject: "Loom cutover schedule",
  body: "Roof access is fine from the 14th.",
  html: "<p>Roof access is fine from the 14th.</p>",
  tz: "AEDT",
  tzOffsetMinutes: 660,
  org: "Loomworks",
  participants: [{ personId: 1, name: "Bo Halvorsen", role: "from" }],
  toRecipients: [{ name: "Cy Okafor", address: "cy@loomworks.example" }],
  ccRecipients: [
    { name: "Carl Nkemdirim", address: "carl@example.net" },
    { name: "Marit Solheim", address: "marit@loomworks.example" },
  ],
  attachments: [],
  permalink: mailbox("g-1"),
  ...over,
});

/** One thread as /v1/chains answers it: a message the mailbox holds, and — for
 *  the tests that need one — a newer line recovered from a quote, which is the
 *  entry nothing can be threaded onto. */
const chainBody = (entries: unknown[]) => ({ rootExtId: ROOT, entries });

/** One message as the corpus holds it, with the people on it. The sender is one
 *  row and the audience is the rest, which is the split a reply makes: the reply
 *  goes to the sender and cc's everyone else, so the two are told apart here the
 *  way the box tells them apart. Marit is the reader — the person /v1/settings
 *  names as "me" — and is on the message the way the mailbox owner usually is,
 *  cc'd by somebody else, which is the case the audience line has to get right.
 *  Carl appears twice because two person rows for one colleague is a thing the
 *  corpus does; one name is what the line prints. */
const onLoom = [
  { personId: 1, name: "Bo Halvorsen", role: "from" },
  { personId: 2, name: "Cy Okafor", role: "to" },
  { personId: 3, name: "Carl Nkemdirim", role: "cc" },
  { personId: 4, name: "Marit Solheim", role: "cc" },
  { personId: 5, name: "Carl Nkemdirim", role: "cc" },
];

const ME = 4;

/** The corpus's identity graph, as /v1/people answers it: the people on the thread,
 *  with the addresses the read of the thread does not carry. Cy is in it with one
 *  address and a name that is not an address; Carl is in it twice, because two
 *  person rows for one colleague is a thing this corpus does, and the two rows
 *  answer with two addresses. Bo is deliberately absent, which is the case of a
 *  name the corpus knows nothing more about than the message already said. Marit is
 *  the reader, which is what makes her the one address a field refuses from the
 *  corpus rather than by the shape of it, and Dana is somebody no message in this
 *  thread has ever reached. */
const PEOPLE = [
  {
    personId: 2,
    displayName: "Cy Okafor",
    identities: ["display_name:cy okafor", "email:cy@loomworks.example"],
    sent: 3,
    received: 9,
  },
  {
    personId: 3,
    displayName: "Carl Nkemdirim",
    identities: ["email:carl@loomworks.example"],
    sent: 1,
    received: 4,
  },
  {
    personId: 5,
    displayName: "Carl Nkemdirim",
    identities: ["email:carl.n@loomworks.example"],
    sent: 0,
    received: 2,
  },
  {
    personId: ME,
    displayName: "Marit Solheim",
    identities: ["email:marit@loomworks.example"],
    sent: 2,
    received: 7,
  },
  // Somebody the thread has never seen, which is the case the address fields exist
  // for: the corpus knows her and the message does not.
  {
    personId: 7,
    displayName: "Dana Whitfield",
    identities: ["email:dana@loomworks.example"],
    sent: 0,
    received: 0,
  },
];

const threaded = [
  entry({ extId: ROOT, participants: onLoom }),
  entry({
    extId: QUOTED,
    author: "Cy Devlin",
    ts: "2026-03-12T09:00:00Z",
    quoted: true,
    body: "and the fitters can come on the 14th",
    permalink: undefined,
  }),
];

/** The HTML part the server composes beside the text, as the plan carries it back:
 *  the reader's words as paragraphs, and the answered message in the blockquote a
 *  client folds. The same message as `body`, marked up — which is the thing the
 *  preview has to draw when that is the form going out. */
const HTML_PART =
  "<p>The 14th works.</p>\n" +
  "<p>On Wed 11 Mar 2026 17:40 AEST, Bo Halvorsen &lt;bo@fjordline.example&gt; wrote:</p>\n" +
  '<blockquote class="gmail_quote">\n<p>Roof access is fine from the 14th.</p>\n</blockquote>\n';

/** The audience the mailbox resolved, as the plan carries it back: the addresses
 *  rather than only the headers they will be written into, which is what the
 *  preview's chips are drawn from and the whole of what a send may name. Bo is the
 *  sender in to; the rest of the message's audience is in cc, each with the name the
 *  answered message gave it — or none, where it carried none. The reader is not on
 *  either list, because the mailbox leaves its own addresses off a reply. */
const TO_RECIPIENTS = [{ name: "Bo Halvorsen", address: "bo@fjordline.example" }];
const CC_RECIPIENTS = [
  { name: "Cy Okafor", address: "cy@loomworks.example" },
  { address: "carl@example.net" },
];

/** What the server answers a send with: the recipient and subject its own headers
 *  give, and the whole body with the quote in it. The same fields for the preview
 *  and the send, which is what lets a client show one and send the other — and the
 *  HTML part only when the reply is going out with it, so its presence is the
 *  response's own answer to which rendering travels. */
const replyPlan = (sent: boolean, html = true) => ({
  entry: ROOT,
  to: "Bo Halvorsen <bo@fjordline.example>",
  cc: "Cy Okafor <cy@loomworks.example>, carl@example.net",
  toRecipients: TO_RECIPIENTS,
  ccRecipients: CC_RECIPIENTS,
  subject: "Re: Loom cutover schedule",
  body:
    "The 14th works.\n\n" +
    "On Wed 11 Mar 2026 17:40 AEST, Bo Halvorsen <bo@fjordline.example> wrote:\n" +
    "> Roof access is fine from the 14th.\n",
  ...(html ? { html: HTML_PART } : {}),
  sent,
  ...(sent ? { gmailId: "g-9" } : {}),
});

/** The page's list, and the endpoints the pane reaches for around it. */
const server = (
  chain: () => Response,
  send?: Handler,
  settings: Record<string, unknown> = { mePersonId: ME },
): Handler => (c) => {
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
  if (p === "/v1/settings") return json(200, settings);
  if (p === "/v1/people") return json(200, { people: PEOPLE });
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
/** The plan's body, whichever form the reply will go out in: the HTML rendering
 *  when the tick is on and the text `<pre>` when it is off. What a test waiting
 *  for "the plan is up" waits for without asserting which form it drew. */
const planBody = () => box()!.querySelector(".replytext, .replyhtml");
/** The addresses the plan's fields hold, in the order they are drawn — the audience
 *  the preview is showing, which is the thing the message that leaves has to agree
 *  with. `list` names which field, because who is in to and who is in cc is half of
 *  what the reader arranged. */
const chips = (list: "to" | "cc") =>
  [...box()!.querySelectorAll<HTMLElement>(`.addrfield[data-list="${list}"] .addrname`)].map(
    (c) => c.textContent!,
  );
/** One address's chip in one list, by the address it prints: the plan's control is
 *  one chip per address, so a test names the address and not a position. */
const chipOf = (list: "to" | "cc", address: string) =>
  [...box()!.querySelectorAll<HTMLElement>(`.addrfield[data-list="${list}"] .addrchip`)].find((c) =>
    c.querySelector(".addrname")!.textContent!.includes(address),
  )!;
/** The press that takes a named address off the reply. */
const chipX = (list: "to" | "cc", address: string) =>
  within(chipOf(list, address)).getByRole("button", { name: /^remove / });
/** The field for a list: the input an address is typed into. */
const addressField = (list: "to" | "cc") =>
  screen.getByLabelText(`${list} addresses`) as HTMLInputElement;
/** Type into a field, as a reader does — the field's own change, which is what
 *  opens the suggestions. */
const type = (list: "to" | "cc", text: string) =>
  fireEvent.change(addressField(list), { target: { value: text } });
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
async function previewWithAudience(call: Call, response: Response): Promise<Response> {
  const body = (await response.json()) as Record<string, unknown>;
  const request = JSON.parse(call.body ?? "{}") as Record<string, unknown>;
  const setList = (field: "to" | "cc", recipientsField: "toRecipients" | "ccRecipients") => {
    const requested = request[field];
    if (!Array.isArray(requested)) return;
    const previous = (body[recipientsField] as { name?: string; address: string }[] | undefined) ?? [];
    const known = new Map(previous.map((r) => [r.address.toLowerCase(), r]));
    const recipients = (requested as string[]).map((address) => known.get(address.toLowerCase()) ?? { address });
    body[recipientsField] = recipients;
    body[field] = recipients.map((r) => r.name ? `${r.name} <${r.address}>` : r.address).join(", ");
  };
  setList("to", "toRecipients");
  setList("cc", "ccRecipients");
  return json(response.status, body);
}

async function write(
  text: string,
  answer: () => Response = () => json(200, replyPlan(false)),
  beforePreview?: () => void | Promise<void>,
) {
  handler = server(
    () => json(200, chainBody(threaded)),
    async (call) => previewWithAudience(call, answer()),
  );
  await mountApp();
  await openThread();
  fireEvent.change(field(), { target: { value: text } });
  await beforePreview?.();
  press("preview");
}

describe("answering a message from the pane", () => {
  it("puts recipient autocomplete in the compose screen and omits the Replying to line", async () => {
    handler = server(() => json(200, chainBody(threaded)));
    await mountApp();
    await openThread();

    await waitFor(() => expect(chips("to")).toEqual(["Bo Halvorsen <bo@fjordline.example>"]));
    expect(chips("cc")).toEqual([
      "Cy Okafor <cy@loomworks.example>",
      "Carl Nkemdirim <carl@example.net>",
    ]);
    expect(chips("cc")).not.toContain("Carl Nkemdirim <carl@loomworks.example>");
    expect(screen.getByLabelText("to addresses")).toBeTruthy();
    expect(screen.getByLabelText("cc addresses")).toBeTruthy();
    expect(box()!.querySelector(".replyto")).toBeNull();
    expect(box()!.textContent).not.toContain("Replying to");
    const row = box()!.querySelector(".replyrecipients")!;
    expect(row.querySelectorAll(".addrfield")).toHaveLength(2);
    expect(row.compareDocumentPosition(field()) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    const target = within(box()!).getByRole("link", { name: /Jump to the message being replied to: Bo Halvorsen/ });
    expect(target.tagName).toBe("A");
    expect(target.classList.contains("opbtn")).toBe(true);
    expect(target.parentElement?.lastElementChild).toBe(target);
    expect(target.getAttribute("href")).toBe("#entry-0");

    // It is a real anchor to the answered bubble, and the shared delegated hover
    // behavior highlights that same target without changing the composer state.
    const message = document.getElementById("entry-0")!;
    fireEvent.mouseOver(target);
    expect(message.classList.contains("mhov")).toBe(true);
    fireEvent.mouseOut(target);
    expect(message.classList.contains("mhov")).toBe(false);

    // Suggestions can be picked before asking for the message preview.
    type("cc", "dana");
    fireEvent.click(screen.getByRole("option", { name: /Dana Whitfield/ }));
    expect(chips("cc")).toContain("Dana Whitfield <dana@loomworks.example>");
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
    expect(sent(0)).toEqual({
      entry: ROOT,
      body: "The 14th works.",
      all: true,
      html: true,
      confirm: false,
    });

    // What the reader is shown is the whole message as it will go, and the first
    // thing it says is that nothing has left yet — a preview that did not say so
    // would be a screen where the reader cannot tell whether they had sent.
    const shown = box()!;
    expect(shown.querySelector(".replynote")!.textContent).toContain("Nothing has been sent yet.");
    expect(shown.querySelector(".replynote")!.textContent).toContain(
      "Bo Halvorsen <bo@fjordline.example>",
    );
    // The actual mailbox audience is stated plainly in the preview; autocomplete
    // stays on the compose screen rather than appearing beside the checked message.
    expect(shown.querySelectorAll(".addrfield")).toHaveLength(0);
    expect(shown.querySelector(".replynote")!.textContent).toContain(
      "cc Cy Okafor <cy@loomworks.example>, carl@example.net",
    );
    expect(shown.querySelector(".replynote")!.textContent).toContain("Re: Loom cutover schedule");
    // And which forms it goes in, since that is the other thing the reader can
    // change and the other thing the two-step is there to let them check.
    expect(shown.querySelector(".replynote")!.textContent).toContain("text and HTML");
    // The body is drawn in the form that is going out, not the other one. With the
    // html tick on the reply travels with its HTML part, so what the reader checks
    // is that rendering — the words as paragraphs and the message being answered
    // inside the blockquote a client folds — rather than the plain-text lines their
    // correspondent will not receive. The text `<pre>` is the drawing for the
    // tick-off case, in the test below.
    const rendered = shown.querySelector(".replyhtml")!;
    expect(shown.querySelector(".replytext")).toBeNull();
    expect(rendered.innerHTML).toContain("<p>The 14th works.</p>");
    expect(rendered.innerHTML).toContain('<blockquote class="gmail_quote">');
    expect(rendered.innerHTML).toContain("<p>Roof access is fine from the 14th.</p>");
    expect(rendered.textContent).toContain("wrote:");
    // Both the text box and recipient autocomplete are hidden while the message is
    // being checked; "keep editing" returns to the initial composer.
    expect(screen.queryByLabelText("Your reply")).toBeNull();
    expect(screen.queryByLabelText("to addresses")).toBeNull();
  });

  it("answers the sender alone when the reply-all tick is cleared", async () => {
    handler = server(() => json(200, chainBody(threaded)));
    await mountApp();
    await openThread();
    await waitFor(() => expect(box()).toBeTruthy());

    // The tick is on by default. Its change updates the untouched Cc list on the
    // compose screen, where the recipients are now arranged.
    const tick = within(box()!).getByRole("checkbox", { name: /reply all/ }) as HTMLInputElement;
    expect(tick.checked).toBe(true);
    expect(chips("cc")).toContain("Cy Okafor <cy@loomworks.example>");
    // It is the left end of the row whose buttons are at the right: the choices
    // under the field, the press past them.
    expect(box()!.querySelector(".replyacts .replyopts .replytick")).toBeTruthy();

    fireEvent.click(tick);
    expect(tick.checked).toBe(false);
    expect(chips("cc")).toEqual([]);

    fireEvent.change(field(), { target: { value: "The 14th works." } });
    press("preview");

    // And the setting is what the server is asked for rather than only what the
    // box says: answering one person when twelve are on the message is the thing
    // this control exists to make possible.
    await waitFor(() => expect(sends()).toHaveLength(1));
    expect(sent(0)).toEqual({
      entry: ROOT,
      body: "The 14th works.",
      all: false,
      html: true,
      confirm: false,
    });
  });

  it("sends the words alone when the html tick is cleared", async () => {
    handler = server(
      () => json(200, chainBody(threaded)),
      () => json(200, replyPlan(false, false)),
    );
    await mountApp();
    await openThread();

    // Two ticks, one row, both on — and the second is a choice about the form of
    // the message rather than about what it says, which is what its label and its
    // title have to make clear enough to tick without fear.
    const ticks = within(box()!).getAllByRole("checkbox") as HTMLInputElement[];
    expect(ticks.map((t) => (t.parentElement!.textContent || "").trim())).toEqual([
      "reply all",
      "send html",
    ]);
    expect(ticks.every((t) => t.checked)).toBe(true);

    fireEvent.click(within(box()!).getByRole("checkbox", { name: /send html/ }));
    fireEvent.change(field(), { target: { value: "The 14th works." } });
    press("preview");

    // What the server is asked for, rather than only what the box says: the form
    // is the reader's to choose, and their correspondents' clients never see a
    // quote they were not sent.
    await waitFor(() => expect(sends()).toHaveLength(1));
    expect(sent(0)).toEqual({
      entry: ROOT,
      body: "The 14th works.",
      all: true,
      html: false,
      confirm: false,
    });
    // Said before the second press as well as sent on it: a preview that named one
    // form and a send that used the other would be the one thing the two-step
    // exists to prevent. The plan is otherwise the same message — the words and the
    // quote are untouched, so the reader's check of them still stands.
    const note = box()!.querySelector(".replynote")!.textContent!;
    expect(note).toContain("plain text alone");
    // And what is drawn agrees with the sentence: no HTML half came back, so the
    // text part is the whole message and it is drawn as the lines that will be
    // sent — the quote marks the server added, which is the thing being checked.
    expect(box()!.querySelector(".replyhtml")).toBeNull();
    expect(box()!.querySelector(".replytext")!.textContent).toContain(
      "> Roof access is fine from the 14th.",
    );

    press("send this reply");
    await waitFor(() => expect(sends()).toHaveLength(2));
    expect(sent(1)).toEqual({
      entry: ROOT,
      body: "The 14th works.",
      all: true,
      html: false,
      confirm: true,
    });
  });

  it("draws an empty cc field when the reply has nobody else on it, and sends no cc", async () => {
    // A message the reader was the only recipient of answers with no cc at all
    // (see the contract's SendResponse). The field is still drawn — an empty cc is
    // exactly the reply a reader may want to add somebody to, and a control that
    // appeared only once there was already a cc could not be the one that adds the
    // first — but nothing is invented to sit in it, and the line claims no cc.
    await write("The 14th works.", () =>
      json(200, { ...replyPlan(false), cc: undefined, ccRecipients: undefined }),
    );

    await waitFor(() => expect(box()!.querySelector(".replynote")).toBeTruthy());
    const note = box()!.querySelector(".replynote")!.textContent!;
    expect(box()!.querySelectorAll(".addrfield")).toHaveLength(0);
    expect(note).toContain("Bo Halvorsen <bo@fjordline.example>");

    // Neither recipient list was edited, so the mailbox keeps the same defaults
    // it used for the preview rather than receiving guessed addresses.
    press("send this reply");
    await waitFor(() => expect(sends()).toHaveLength(2));
    expect(sent(1)).not.toHaveProperty("cc");
  });

  it("prints the mailbox's own headers when the plan carried no recipient addresses", async () => {
    // The two say the same fact, and a plan that carried only the first has nothing
    // to seed a field with. It is printed as it came rather than taken apart into
    // addresses here: splitting a display line on commas would be this pane's
    // second, worse reading of who a message reached (see the `audience` tests
    // below), and the fields are only drawn for a plan that named its recipients.
    await write("The 14th works.", () =>
      json(200, { ...replyPlan(false), toRecipients: undefined, ccRecipients: undefined }),
    );

    await waitFor(() => expect(box()!.querySelector(".replynote")).toBeTruthy());
    expect(box()!.querySelectorAll(".addrfield")).toHaveLength(0);
    expect(box()!.querySelector(".replynote")!.textContent).toContain(
      "Cy Okafor <cy@loomworks.example>, carl@example.net",
    );
  });

  it("edits recipients before preview and sends the arranged Cc list", async () => {
    await write("The 14th works.", () => json(200, replyPlan(false)), async () => {
      await waitFor(() => expect(chips("cc")).toContain("Cy Okafor <cy@loomworks.example>"));
      fireEvent.click(chipX("cc", "cy@loomworks.example"));
      type("cc", "cy");
      fireEvent.click(screen.getByRole("option", { name: /Cy Okafor/ }));
      expect(chips("cc")).toContain("Cy Okafor <cy@loomworks.example>");
      fireEvent.click(chipX("cc", "cy@loomworks.example"));
      expect(chips("cc")).not.toContain("Cy Okafor <cy@loomworks.example>");
    });

    await waitFor(() => expect(sends()).toHaveLength(1));
    expect(sent(0)).toEqual({
      entry: ROOT,
      body: "The 14th works.",
      all: true,
      html: true,
      confirm: false,
      cc: ["carl@example.net"],
    });
    expect(box()!.querySelectorAll(".addrfield")).toHaveLength(0);
    press("send this reply");
    await waitFor(() => expect(sends()).toHaveLength(2));
    expect(sent(1)).toEqual({
      entry: ROOT,
      body: "The 14th works.",
      all: true,
      html: true,
      confirm: true,
      cc: ["carl@example.net"],
    });
  });

  it("moves an address between recipient lists by removing and re-adding it", async () => {
    await write("The 14th works.", () => json(200, replyPlan(false)), async () => {
      await waitFor(() => expect(chips("cc")).toContain("Cy Okafor <cy@loomworks.example>"));
      fireEvent.click(chipX("cc", "cy@loomworks.example"));
      type("to", "cy");
      fireEvent.click(screen.getByRole("option", { name: /Cy Okafor/ }));
      expect(chips("to")).toEqual([
        "Bo Halvorsen <bo@fjordline.example>",
        "Cy Okafor <cy@loomworks.example>",
      ]);
      expect(chips("cc")).toEqual(["Carl Nkemdirim <carl@example.net>"]);
      expect(box()!.querySelectorAll(".addrmove")).toHaveLength(0);
    });

    await waitFor(() => expect(sends()).toHaveLength(1));
    expect(sent(0).to).toEqual(["bo@fjordline.example", "cy@loomworks.example"]);
    expect(sent(0).cc).toEqual(["carl@example.net"]);
    press("send this reply");
    await waitFor(() => expect(sends()).toHaveLength(2));
    expect(sent(1).to).toEqual(["bo@fjordline.example", "cy@loomworks.example"]);
    expect(sent(1).cc).toEqual(["carl@example.net"]);
  });

  it("requires a To recipient after the reader explicitly removes them all", async () => {
    handler = server(
      () => json(200, chainBody(threaded)),
      (call) => previewWithAudience(call, json(200, replyPlan(false))),
    );
    await mountApp();
    await openThread();
    await waitFor(() => expect(chips("to")).toContain("Bo Halvorsen <bo@fjordline.example>"));
    fireEvent.click(chipX("to", "bo@fjordline.example"));
    fireEvent.change(field(), { target: { value: "The 14th works." } });

    const preview = within(box()!).getByRole("button", { name: "preview" }) as HTMLButtonElement;
    expect(preview.disabled).toBe(true);
    expect(preview.title).toContain("somebody in to");
    expect(sends()).toHaveLength(0);

    // Removing the last To address leaves it empty; an address can be added from
    // Cc by removing it there first, then selecting it in To.
    await waitFor(() => expect(chips("cc")).toContain("Cy Okafor <cy@loomworks.example>"));
    fireEvent.click(chipX("cc", "cy@loomworks.example"));
    type("to", "cy");
    fireEvent.click(screen.getByRole("option", { name: /Cy Okafor/ }));
    expect(preview.disabled).toBe(false);
    fireEvent.click(preview);
    await waitFor(() => expect(box()!.querySelector(".replyplan")).toBeTruthy());
    press("send this reply");
    await waitFor(() => expect(sends()).toHaveLength(2));
    expect(sent(1).to).toEqual(["cy@loomworks.example"]);
  });

  it("lets the reader add a known or typed recipient before preview", async () => {
    await write("The 14th works.", () => json(200, replyPlan(false)), async () => {
      await waitFor(() => expect(chips("to")).toContain("Bo Halvorsen <bo@fjordline.example>"));
      type("cc", "dana");
      fireEvent.click(screen.getByRole("option", { name: /Dana Whitfield/ }));
      type("to", "stranger@example.org");
      fireEvent.keyDown(addressField("to"), { key: "Enter" });
      expect(chips("to")).toEqual([
        "Bo Halvorsen <bo@fjordline.example>",
        "stranger@example.org",
      ]);
      expect(chips("cc")).toContain("Dana Whitfield <dana@loomworks.example>");
      expect(addressField("to").value).toBe("");
    });

    await waitFor(() => expect(sends()).toHaveLength(1));
    expect(sent(0).to).toEqual(["bo@fjordline.example", "stranger@example.org"]);
    expect(sent(0).cc).toEqual([
      "cy@loomworks.example",
      "carl@example.net",
      "dana@loomworks.example",
    ]);
    expect(box()!.querySelectorAll(".addrfield")).toHaveLength(0);
    press("send this reply");
    await waitFor(() => expect(sends()).toHaveLength(2));
    expect(sent(1).to).toEqual(["bo@fjordline.example", "stranger@example.org"]);
    expect(sent(1).cc).toEqual([
      "cy@loomworks.example",
      "carl@example.net",
      "dana@loomworks.example",
    ]);
  });

  it("refuses the reader's own address and duplicates while composing", async () => {
    handler = server(() => json(200, chainBody(threaded)));
    await mountApp();
    await openThread();
    await waitFor(() => expect(chips("cc")).toContain("Cy Okafor <cy@loomworks.example>"));

    type("to", "marit@loomworks.example");
    fireEvent.keyDown(addressField("to"), { key: "Enter" });
    expect(box()!.querySelector(".addrrefuse")!.textContent).toContain("your own address");

    type("to", "cy@loomworks.example");
    fireEvent.keyDown(addressField("to"), { key: "Enter" });
    expect(box()!.querySelector(".addrrefuse")!.textContent).toContain("already on the reply");
    expect(chips("to")).toEqual(["Bo Halvorsen <bo@fjordline.example>"]);
    expect(sends()).toHaveLength(0);
  });

  it("sends the body the reader was shown, and re-reads the trail it lands in", async () => {
    await write("The 14th works.");
    await waitFor(() => expect(sends()).toHaveLength(1));
    const before = chains().length;

    fireEvent.click(within(box()!).getByRole("button", { name: "send this reply" }));

    await waitFor(() => expect(sends()).toHaveLength(2));
    expect(sent(1)).toEqual({
      entry: ROOT,
      body: "The 14th works.",
      all: true,
      html: true,
      confirm: true,
    });
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
    expect(planBody()).toBeNull();
    expect(field().value).toBe("");
  });

  it("says it once, and says the last one", async () => {
    await write("The 14th works.");
    await waitFor(() => expect(planBody()).toBeTruthy());
    press("send this reply");
    await waitFor(() => expect(document.querySelectorAll(".toast")).toHaveLength(1));

    // A second reply is a second send, and the corner holds the account of the
    // last thing this box did rather than one sentence per reply — a reply cannot
    // be recalled, so two accounts of two of them is a reader counting.
    fireEvent.change(field(), { target: { value: "And the fitters?" } });
    press("preview");
    await waitFor(() => expect(planBody()).toBeTruthy());
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

/**
 * Answering an older message: the press in a header, and the box it moves.
 *
 * The box answers the newest answerable message in a thread by itself, which is
 * right until it is not — the message a reader wants to answer is often the one
 * that asked them something, and the newest line of a thread is frequently a
 * reply that asked nothing. What is asserted here is the whole of that choice:
 * which message is named, that the audience is the one the press names (reply
 * all, so the tick goes on), that the press can only name a message the mailbox
 * holds, and that a preview prepared for one message cannot be sent against
 * another.
 */

const FIRST = "mail:<loom-cutover-0@example.fed>";

/** Two messages the mailbox holds, which is the case the choice exists for: an
 *  older one from Cy asking, and Bo's answer after it — the box answering Bo by
 *  itself, and Cy being the one a reader has to be able to name. */
const exchange = [
  entry({
    extId: FIRST,
    ts: "2026-03-02T09:15:00Z",
    author: "Cy Devlin",
    fromEmail: "cy@loomworks.example",
    subject: "Loom cutover schedule?",
    body: "Can the fitters come on the 14th?",
    html: "<p>Can the fitters come on the 14th?</p>",
    permalink: mailbox("g-0"),
    participants: [
      { personId: 6, name: "Cy Devlin", role: "from" },
      { personId: 1, name: "Bo Halvorsen", role: "to" },
      { personId: ME, name: "Marit Solheim", role: "cc" },
    ],
  }),
  entry({ extId: ROOT, participants: onLoom }),
];

/** The bubble a message is drawn as, by the sender its head names. */
const bubbleOf = (author: string) =>
  [...pane().querySelectorAll<HTMLElement>(".msg")].find(
    (m) => m.querySelector(".nm")!.textContent === author,
  )!;

/** The press that aims the box at that message, as the pane draws it — in the
 *  receipt, beside the controls that copy the message and switch its rendering. */
const aimAt = (author: string) => {
  const btn = bubbleOf(author).querySelector<HTMLButtonElement>(".hdetend .replyall")!;
  fireEvent.click(btn);
  return btn;
};

describe("answering an older message from its own header", () => {
  it("tracks the target message on its reply control without a Replying to line", async () => {
    handler = server(() => json(200, chainBody(exchange)));
    await mountApp();
    await openThread();

    // The newest answerable message is selected, while the Replying to header is
    // deliberately absent from the composer.
    expect(box()!.querySelector(".replyto")).toBeNull();
    // The press on that message reads as the state it is: the box is answering
    // it, so the one press in the thread that says which is the pressed one.
    expect(bubbleOf("Bo Halvorsen").querySelector(".replyall")!.getAttribute("aria-pressed")).toBe(
      "true",
    );
    expect(bubbleOf("Cy Devlin").querySelector(".replyall")!.getAttribute("aria-pressed")).toBe(
      "false",
    );

    aimAt("Cy Devlin");
    await waitFor(() =>
      expect(bubbleOf("Cy Devlin").querySelector(".replyall")!.getAttribute("aria-pressed")).toBe(
        "true",
      ),
    );
    expect(bubbleOf("Cy Devlin").querySelector(".replyall")!.getAttribute("aria-pressed")).toBe(
      "true",
    );
    expect(bubbleOf("Bo Halvorsen").querySelector(".replyall")!.getAttribute("aria-pressed")).toBe(
      "false",
    );
    expect(box()!.querySelector(".replytarget")?.getAttribute("href")).toBe(
      `#${bubbleOf("Cy Devlin").id}`,
    );

    // And the message the box will answer is the one the server is asked about,
    // which is the only claim that matters: everything else here is words.
    fireEvent.change(field(), { target: { value: "Yes — the 14th." } });
    press("preview");
    await waitFor(() => expect(sends()).toHaveLength(1));
    expect(sent(0)).toEqual({
      entry: FIRST,
      body: "Yes — the 14th.",
      all: true,
      html: true,
      confirm: false,
    });
  });

  it("turns the reply-all tick on, because that is what the press says", async () => {
    handler = server(() => json(200, chainBody(exchange)));
    await mountApp();
    await openThread();

    // A reader who had taken everyone else off the reply, and then asks to answer
    // an older message *and* says reply all: the audience is the press's own word,
    // so it is what the press sets rather than a tick left where it was.
    fireEvent.click(within(box()!).getByRole("checkbox", { name: /reply all/ }));
    expect(chips("cc")).toEqual([]);

    aimAt("Cy Devlin");
    const tick = within(box()!).getByRole("checkbox", { name: /reply all/ }) as HTMLInputElement;
    await waitFor(() => expect(tick.checked).toBe(true));
    // The checkbox follows the selected target; no separate Replying to sentence
    // is needed to show which message will be answered.
    expect(box()!.querySelector(".replyto")).toBeNull();
  });

  it("brings the box up to the reader who pressed it", async () => {
    handler = server(() => json(200, chainBody(exchange)));
    await mountApp();
    await openThread();

    // The box is at the bottom of the thread and the message pressed may be
    // screens above it, so the press is what brings the box into view — and the
    // field takes the cursor, because the press was an intent to write. jsdom
    // scrolls nothing, so what is asserted is that the box asked.
    const scrolled: string[] = [];
    const scrolledBefore = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = function (this: Element) {
      scrolled.push(this.className);
    };
    try {
      aimAt("Cy Devlin");
      await waitFor(() => expect(scrolled).toContain("replybox"));
      expect(document.activeElement).toBe(field());
    } finally {
      Element.prototype.scrollIntoView = scrolledBefore;
    }
  });

  it("drops a preview prepared for the message it no longer answers", async () => {
    handler = server(
      () => json(200, chainBody(exchange)),
      () => json(200, replyPlan(false)),
    );
    await mountApp();
    await openThread();
    fireEvent.change(field(), { target: { value: "The 14th works." } });
    press("preview");
    await waitFor(() => expect(box()!.querySelector(".replyplan")).toBeTruthy());

    // A plan is a claim about the message the box was answering when it was made:
    // recipients, subject and a quote of that message in the body the reader was
    // shown. Retarget the box and the claim is about a message it no longer names
    // — and the second press recomposes the body for the new target, so the one
    // thing that must not survive the move is the screen the reader checked.
    aimAt("Cy Devlin");
    await waitFor(() => expect(box()!.querySelector(".replyplan")).toBeNull());
    expect(planBody()).toBeNull();
    // The words are still the reader's, and they stay where the reader can send
    // them again — the press changed which message they answer, not what they say.
    expect(field().value).toBe("The 14th works.");
    expect(box()!.querySelector(".replyto")).toBeNull();
  });

  it("draws no press on a message the mailbox does not hold", async () => {
    handler = server(() => json(200, chainBody(threaded)));
    await mountApp();
    await openThread();
    await waitFor(() => expect(bubbleOf("Bo Halvorsen")).toBeTruthy());

    // The thread above carries a line recovered from somebody's quote, which is the
    // entry nothing can be threaded onto: there is no mailbox copy to reply to, so
    // there is no press — rather than a press the server would refuse by name.
    expect(bubbleOf("Bo Halvorsen").querySelector(".replyall")).toBeTruthy();
    expect(bubbleOf("Cy Devlin").querySelector(".replyall")).toBeNull();
  });
});

describe("the press's own drawing", () => {
  it("draws a glyph and keeps its words in the label", async () => {
    handler = server(() => json(200, chainBody(exchange)));
    await mountApp();
    await openThread();
    await waitFor(() => expect(bubbleOf("Bo Halvorsen")).toBeTruthy());

    // The receipt's controls are icon buttons (see the stylesheet's shared rule), so
    // the press is a drawn reply-all whose words are its label and its title. What is
    // pinned here is that much and no more: the shape is a drawing, and a test that
    // fixed its coordinates would make the next drawing of it a test rewrite rather
    // than a change of one path.
    const press = bubbleOf("Cy Devlin").querySelector(".replyall")!;
    expect(press.textContent).toBe("");
    expect(press.getAttribute("title")).toContain("Reply all");
    expect(press.getAttribute("aria-label")).toContain("Reply all");
    // The message the box already answers wears the other label, which is state
    // rather than a second name for the press.
    const pressed = bubbleOf("Bo Halvorsen").querySelector(".replyall")!;
    expect(pressed.getAttribute("title")).toContain("the box below is answering");
    const paths = [...press.querySelectorAll("svg path")];
    // Two heads and one tail, and nothing filled: the app's icons are strokes.
    expect(paths).toHaveLength(3);
    expect(paths.every((p) => p.getAttribute("fill") === "none")).toBe(true);
    expect(press.querySelector("svg")!.getAttribute("width")).toBe("18");
  });
});
