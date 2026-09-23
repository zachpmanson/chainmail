// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { makeQueryClient } from "../src/lib/queryClient";
import { clearToasts } from "../src/lib/toasts";
import { createChainmailRouter } from "../src/router";
import { audience } from "../src/components/ReplyBox";

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
const addresses = () => [...chips("to"), ...chips("cc")];
/** One address's chip in one list, by the address it prints: the plan's control is
 *  one chip per address, so a test names the address and not a position. */
const chipOf = (list: "to" | "cc", address: string) =>
  [...box()!.querySelectorAll<HTMLElement>(`.addrfield[data-list="${list}"] .addrchip`)].find((c) =>
    c.querySelector(".addrname")!.textContent!.includes(address),
  )!;
/** The press that takes an address off the reply, and the one that sends it in the
 *  other list. Both are named by the address they act on (see AddressField). */
const chipX = (list: "to" | "cc", address: string) =>
  within(chipOf(list, address)).getByRole("button", { name: /^remove / });
const chipMove = (list: "to" | "cc", address: string) =>
  within(chipOf(list, address)).getByRole("button", { name: / instead$/ });
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
    // And the reply reaches the message's whole audience, which the line names: the
    // sender in its own clause and the rest of the message's people as the cc they
    // will be. The reader is on that message and is not on that list — a reply is
    // not addressed back to the person writing it — and a colleague the corpus holds
    // two rows for is one name.
    expect(to).toContain("cc Cy Okafor and Carl Nkemdirim");
    expect(to).not.toContain("Marit Solheim");
    // The words are the bubble's own, which is the point of the pairing: the head
    // above says when the message arrived, and this line must not say otherwise.
    expect(sends()).toHaveLength(0);
  });

  it("says where each name it will cc answers to, without changing the sentence", async () => {
    handler = server(() => json(200, chainBody(threaded)));
    await mountApp();
    await openThread();

    // A name on its own is not a claim a reader can check, and the address of a
    // person this thread was *sent to* is nowhere in the read: the entry carries a
    // person id and a name and no address (see castOfEntries), so the corpus's
    // identity graph is what answers. The line's own words are unchanged — this is
    // a hover, not a second line of text — which is what the sentence assertions
    // above are still watching for.
    const line = () => box()!.querySelector(".replyto") as HTMLElement;
    await waitFor(() =>
      expect(within(line()).getByText("Cy Okafor").getAttribute("title")).toBe(
        "Cy Okafor <cy@loomworks.example>",
      ),
    );
    // Two rows, two addresses, one name: both are listed, because the reader is
    // hovering a name that stands for both of them.
    expect(within(line()).getByText("Carl Nkemdirim").getAttribute("title")).toBe(
      "Carl Nkemdirim <carl@loomworks.example, carl.n@loomworks.example>",
    );
    // And a name nothing has an address for is left as a name: the title is the name
    // itself, which is the fallback every hover in this app takes (see lib/who)
    // rather than an empty tooltip or an invented address.
    expect(within(line()).getByText("Bo Halvorsen").getAttribute("title")).toBe("Bo Halvorsen");
    expect(line().querySelectorAll("[title='']").length).toBe(0);
    expect(line().textContent).toContain("cc Cy Okafor and Carl Nkemdirim");
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
    // Everyone else is named too, because a reply-all's plan is only checkable if
    // the reader can see who else is on it — the one thing the preview exists for.
    // Each address is a chip of its own in the field for the list it is in, rather
    // than a sentence of comma-separated text, because these fields are the control
    // the reader arranges the reply with — and what they print is the address the
    // mailbox resolved rather than a name this pane would have had to guess an
    // address for.
    expect(chips("to")).toEqual(["Bo Halvorsen <bo@fjordline.example>"]);
    expect(chips("cc")).toEqual(["Cy Okafor <cy@loomworks.example>", "carl@example.net"]);
    expect(shown.querySelector(".replynote")!.textContent).toContain("cc");
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
    expect(box()!.querySelector(".replyto")!.textContent).toContain("cc Cy Okafor");
    // It is the left end of the row whose buttons are at the right: the choices
    // under the field, the press past them.
    expect(box()!.querySelector(".replyacts .replyopts .replytick")).toBeTruthy();

    fireEvent.click(tick);
    expect(tick.checked).toBe(false);
    const said = box()!.querySelector(".replyto")!.textContent!;
    expect(said).toContain("nobody else");
    // The names go with the tick, because the names were the tick's own claim about
    // the reply: leaving them up would have the box promise a cc the send no longer
    // carries, which is the one lie this line exists to prevent.
    expect(said).not.toContain("Cy Okafor");
    expect(said).not.toContain("Carl Nkemdirim");

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
      // The audience goes out with the send as the chips showed it, address by
      // address — which is what makes the message that leaves the one that was
      // checked, and what the mailbox arranges rather than restates.
      to: ["bo@fjordline.example"],
      cc: ["cy@loomworks.example", "carl@example.net"],
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
    expect(chips("to")).toEqual(["Bo Halvorsen <bo@fjordline.example>"]);
    expect(chips("cc")).toEqual([]);
    expect(note).toContain("Bo Halvorsen <bo@fjordline.example>");

    // And the send names no cc, which is what the mailbox would have arranged on
    // its own: the reader changed nothing, so the reply is the one that was
    // previewed.
    press("send this reply");
    await waitFor(() => expect(sends()).toHaveLength(2));
    expect(sent(1).cc).toEqual([]);
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

  it("takes an address off the reply when its chip is removed, and sends the lists' audience", async () => {
    // The message had a third party in cc and the reader does not want them on the
    // answer. One press is the whole of that choice: the address leaves the field
    // the reader is checking, which is the thing that has to be true — and it is
    // still offered as a suggestion, because an address the message carried is an
    // address this reply may still carry.
    await write("The 14th works.");
    await waitFor(() => expect(box()!.querySelector(".replyplan")).toBeTruthy());

    fireEvent.click(chipX("cc", "cy@loomworks.example"));

    expect(chips("to")).toEqual(["Bo Halvorsen <bo@fjordline.example>"]);
    expect(chips("cc")).toEqual(["carl@example.net"]);
    expect(box()!.querySelector(".replynote")!.textContent).not.toContain("cy@loomworks.example");

    // And it can be put back by typing it, which is the same field doing the same
    // thing: nothing about the audience is re-decided on the way out.
    type("cc", "cy");
    fireEvent.click(screen.getByRole("option", { name: /Cy Okafor/ }));
    expect(chips("cc")).toEqual(["carl@example.net", "Cy Okafor <cy@loomworks.example>"]);
    fireEvent.click(chipX("cc", "cy@loomworks.example"));

    // The send names the two lists the fields hold.
    press("send this reply");
    await waitFor(() => expect(sends()).toHaveLength(2));
    expect(sent(1)).toEqual({
      entry: ROOT,
      body: "The 14th works.",
      all: true,
      html: true,
      confirm: true,
      to: ["bo@fjordline.example"],
      cc: ["carl@example.net"],
    });
  });

  it("moves an address between cc and to, and sends it where the reader put it", async () => {
    await write("The 14th works.");
    await waitFor(() => expect(box()!.querySelector(".replyplan")).toBeTruthy());

    fireEvent.click(chipMove("cc", "cy@loomworks.example"));
    expect(chips("to")).toEqual([
      "Bo Halvorsen <bo@fjordline.example>",
      "Cy Okafor <cy@loomworks.example>",
    ]);
    expect(chips("cc")).toEqual(["carl@example.net"]);
    // The press beside it names the list the address would go to, so the direction
    // the reader has it in is legible on the chip: it offered "to" while it was in
    // cc, and now it is in to and offers "cc".
    expect(chipMove("to", "cy@loomworks.example").textContent).toBe("cc");

    press("send this reply");
    await waitFor(() => expect(sends()).toHaveLength(2));
    expect(sent(1).to).toEqual(["bo@fjordline.example", "cy@loomworks.example"]);
    expect(sent(1).cc).toEqual(["carl@example.net"]);
  });

  it("will not send a reply with nobody in to, and says why", async () => {
    // Bo is the only address in to, and the mailbox refuses a reply with nobody on
    // it. Taking the last one out is still the reader's to do — they may be about to
    // type another — so the refusal is on the press rather than on the control: it
    // is disabled while to is empty and its title says what is missing, which is a
    // thing the reader can act on rather than a press that fails.
    await write("The 14th works.");
    await waitFor(() => expect(box()!.querySelector(".replyplan")).toBeTruthy());

    fireEvent.click(chipX("to", "bo@fjordline.example"));
    expect(chips("to")).toEqual([]);
    expect(box()!.querySelector(".replynote")!.textContent).toContain("nobody in to");

    const go = within(box()!).getByRole("button", { name: "send this reply" }) as HTMLButtonElement;
    expect(go.disabled).toBe(true);
    expect(go.title).toContain("A reply needs somebody in to");

    // And moving one in from cc puts the press back, because the audience is still
    // the reader's to arrange whichever way they arrange it.
    fireEvent.click(chipMove("cc", "carl@example.net"));
    expect(chips("to")).toEqual(["carl@example.net"]);
    expect(go.disabled).toBe(false);

    press("send this reply");
    await waitFor(() => expect(sends()).toHaveLength(2));
    expect(sent(1).to).toEqual(["carl@example.net"]);
  });

  it("lets the reader name somebody the message never carried, and sends to them", async () => {
    // The widening, end to end. Dana is a person the corpus knows and this thread
    // has never reached, and a stranger is somebody no corpus holds at all — and
    // both go on the reply, one picked and one typed. Nothing is re-decided on the
    // way out: the send names the two lists the fields hold, which is what makes
    // the message that leaves the one the reader arranged.
    await write("The 14th works.");
    await waitFor(() => expect(box()!.querySelector(".replyplan")).toBeTruthy());

    type("cc", "dana");
    fireEvent.click(screen.getByRole("option", { name: /Dana Whitfield/ }));
    expect(addresses()).toEqual([
      "Bo Halvorsen <bo@fjordline.example>",
      "Cy Okafor <cy@loomworks.example>",
      "carl@example.net",
      "Dana Whitfield <dana@loomworks.example>",
    ]);

    type("to", "stranger@example.org");
    fireEvent.keyDown(addressField("to"), { key: "Enter" });
    expect(chips("to")).toEqual(["Bo Halvorsen <bo@fjordline.example>", "stranger@example.org"]);
    // The field the reader typed in is back to its placeholder rather than holding
    // the address it just took: what they typed is a chip now, and leaving the text
    // there would read as an address that did not take.
    expect(addressField("to").value).toBe("");

    press("send this reply");
    await waitFor(() => expect(sends()).toHaveLength(2));
    expect(sent(1).to).toEqual(["bo@fjordline.example", "stranger@example.org"]);
    expect(sent(1).cc).toEqual([
      "cy@loomworks.example",
      "carl@example.net",
      "dana@loomworks.example",
    ]);
  });

  it("refuses the reader's own address and one that is already on the reply, in words", async () => {
    // The two refusals the fields make, and neither is the server's: one recipient
    // is one address in one list, and a reply is not sent to the person writing it.
    // The reader is told at the keyboard rather than being handed a press that
    // fails on the way out — and their text is left where it is, because a field
    // that answered a press with nothing reads as broken.
    await write("The 14th works.");
    await waitFor(() => expect(box()!.querySelector(".replyplan")).toBeTruthy());

    // The corpus knows which address is the reader's from the same settings read
    // the line above the box uses: Marit is the person /v1/settings names as "me".
    type("to", "marit@loomworks.example");
    fireEvent.keyDown(addressField("to"), { key: "Enter" });
    expect(chips("to")).toEqual(["Bo Halvorsen <bo@fjordline.example>"]);
    expect(box()!.querySelector(".addrrefuse")!.textContent).toContain("your own address");

    // And one that is already in the other list, which is the same address twice.
    type("to", "carl@example.net");
    fireEvent.keyDown(addressField("to"), { key: "Enter" });
    expect(chips("to")).toEqual(["Bo Halvorsen <bo@fjordline.example>"]);
    expect(box()!.querySelector(".addrrefuse")!.textContent).toContain("already on the reply");

    // Nothing was sent, and nothing was added: a refusal is the whole of what
    // happened.
    expect(sends()).toHaveLength(1);
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
      // Sent as it was previewed: the audience the plan showed, named address by
      // address, so the second press cannot reach somebody the first one did not.
      to: ["bo@fjordline.example"],
      cc: ["cy@loomworks.example", "carl@example.net"],
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
 * The audience line's own arithmetic, without a pane around it: which people on a
 * message a reply-all would reach. The component tests above cover the sentence it
 * ends up in; what is checked here is the reading it is built on, where the ways to
 * get it wrong are all silent — a name printed twice, the sender listed as somebody
 * the reply is also cc'ing, the reader listed among the recipients.
 */
describe("the people a reply-all reaches", () => {
  const e = (participants: unknown[]) =>
    entry({ participants }) as Parameters<typeof audience>[0];

  it("names the message's people, and neither the sender nor the reader", () => {
    expect(audience(e(onLoom), ME)).toEqual(["Cy Okafor", "Carl Nkemdirim"]);
  });

  it("subtracts nobody when the reader has not said who they are", () => {
    // The setting is the only place "which of these people is me" is answered, so
    // a reader who has named nobody gets the message's own audience — which is the
    // honest reading rather than a guess at an address that might be theirs.
    expect(audience(e(onLoom), undefined)).toEqual([
      "Cy Okafor",
      "Carl Nkemdirim",
      "Marit Solheim",
    ]);
  });

  it("is empty when the message was to nobody but the reader", () => {
    // A message sent to the mailbox alone, and answered: there is no cc to name,
    // which the box says in words rather than with an empty list.
    expect(audience(e([{ personId: 1, name: "Bo Halvorsen", role: "from" }]), ME)).toEqual([]);
  });

  it("names the recipient of a message the reader wrote themselves", () => {
    // The reader's own message, answered: the from row is the reader and is skipped
    // as the sender, and the To row is somebody else and is named.
    const mine = e([
      { personId: ME, name: "Marit Solheim", role: "from" },
      { personId: 1, name: "Bo Halvorsen", role: "to" },
    ]);
    expect(audience(mine, ME)).toEqual(["Bo Halvorsen"]);
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

const toLine = () => box()!.querySelector(".replyto")!.textContent!;

describe("answering an older message from its own header", () => {
  it("names the newest answerable message until a reader asks for another", async () => {
    handler = server(() => json(200, chainBody(exchange)));
    await mountApp();
    await openThread();

    // The default, said in words: the newest message the mailbox holds, which is
    // Bo's answer and not Cy's question above it.
    expect(toLine()).toContain("Bo Halvorsen");
    expect(toLine()).toContain("the newest message here the mailbox holds");
    // And the press on that message reads as the state it is: the box is answering
    // it, so the one press in the thread that says which is the pressed one.
    expect(bubbleOf("Bo Halvorsen").querySelector(".replyall")!.getAttribute("aria-pressed")).toBe(
      "true",
    );
    expect(bubbleOf("Cy Devlin").querySelector(".replyall")!.getAttribute("aria-pressed")).toBe(
      "false",
    );

    aimAt("Cy Devlin");
    await waitFor(() => expect(toLine()).toContain("Cy Devlin"));
    // The line still says which message this is, because it is no longer the one a
    // reader would guess: the box answers what the reader named, and says so.
    expect(toLine()).toContain("not the newest one here");
    expect(toLine()).toContain("Mon, 2 Mar 2026");
    expect(bubbleOf("Cy Devlin").querySelector(".replyall")!.getAttribute("aria-pressed")).toBe(
      "true",
    );
    expect(bubbleOf("Bo Halvorsen").querySelector(".replyall")!.getAttribute("aria-pressed")).toBe(
      "false",
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
    expect(toLine()).toContain("nobody else");

    aimAt("Cy Devlin");
    const tick = within(box()!).getByRole("checkbox", { name: /reply all/ }) as HTMLInputElement;
    await waitFor(() => expect(tick.checked).toBe(true));
    // The names come back with the tick, and they are the answered message's own
    // people: Bo was addressed by Cy's question and is on that reply.
    expect(toLine()).toContain("cc Bo Halvorsen");
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
    expect(toLine()).toContain("Cy Devlin");
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
