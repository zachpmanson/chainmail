// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { CorpusEntry } from "../src/lib/api";
import { makeQueryClient } from "../src/lib/queryClient";
import { addressesOf, receiptNames, senderTitle, usePersonAddresses, withAddress } from "../src/lib/who";

/**
 * Where a name's address comes from: the two answers this app has, and what it
 * says when it has neither.
 *
 * The distinction the module exists for is asserted here rather than only through
 * the components that use it: a *message* is asked what address it came from (its
 * own From header, or the honest note that a line recovered from a quote has
 * none), while a *name* is asked which addresses the person answers to (the
 * corpus's identity graph) — and a reader hovering a name must never be told a
 * guess, because an address reached by matching a name is not evidence about a
 * person, only about a name.
 */

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const entry = (over: Partial<CorpusEntry>): CorpusEntry =>
  ({ extId: "mail:<who-1@loomworks.example>", source: "mail", ts: "2026-03-02T09:15:00Z", ...over }) as CorpusEntry;

describe("the addresses in a person's identities", () => {
  it("keeps the addresses, in order, and drops everything else the corpus keeps", () => {
    // The graph holds more than addresses: a display name a mail arrived with, the
    // spelling of a name, a Slack uid. A hover that offered "zach manson" as an
    // address would be wrong in a way the reader cannot check.
    expect(
      addressesOf([
        "display_name:zach manson",
        "email:zach@termina.io",
        "slack_uid:U123",
        "email:zach+test@termina.io",
      ]),
    ).toEqual(["zach@termina.io", "zach+test@termina.io"]);
    // An address the corpus holds twice is one address, and nothing held at all is
    // no addresses rather than an empty one.
    expect(addressesOf(["email:a@b.example", "email:a@b.example"])).toEqual(["a@b.example"]);
    expect(addressesOf(undefined)).toEqual([]);
    expect(addressesOf(["email:", "display_name:Ada"])).toEqual([]);
  });
});

describe("a name with the addresses behind it", () => {
  it("reads as \"Name <address>\", every address when there are several", () => {
    expect(withAddress("Ada Okoye", ["ada@loomworks.example"])).toBe("Ada Okoye <ada@loomworks.example>");
    expect(withAddress("Ada Okoye", ["ada@loomworks.example", "a.okoye@example.net"])).toBe(
      "Ada Okoye <ada@loomworks.example, a.okoye@example.net>",
    );
  });

  it("is the name alone when there is no address, which is not the same as empty", () => {
    // The fallback every hover in this app takes: a tooltip that repeats the name
    // says less than one with an address, and says it without inventing anything.
    expect(withAddress("Ada Okoye", [])).toBe("Ada Okoye");
    expect(withAddress("Ada Okoye", undefined)).toBe("Ada Okoye");
  });
});

describe("what hovering the sender of a message says", () => {
  it("names the address the message came from", () => {
    expect(senderTitle(entry({ author: "Lena Whitfield", fromEmail: "lane@whitfield.example" }))).toBe(
      "Lena Whitfield <lane@whitfield.example>",
    );
    // An entry with no name is the address as it stands, and one with neither is the
    // name it has, which may be nothing at all.
    expect(senderTitle(entry({ fromEmail: "lane@whitfield.example" }))).toBe("lane@whitfield.example");
    expect(senderTitle(entry({ author: "Lena Whitfield" }))).toBe("Lena Whitfield");
  });

  it("says the address is unknown, and whose address the one in reach is, for a quoted line", () => {
    // A message recovered from inside somebody else's quote has no From header of
    // its own, and the corpus will not lend it one: naming the quoter's address as
    // the sender's would be a claim the evidence does not support, so the absence is
    // named and the address is labelled as theirs.
    expect(senderTitle(entry({ author: "Ada Okoye", fromQuotedBy: "Bo Halvorsen <bo@fjordline.example>" }))).toBe(
      "Ada Okoye (address unknown; quoted by Bo Halvorsen <bo@fjordline.example>)",
    );
    // Nothing to hang the parenthesis on when the line has no name either.
    expect(senderTitle(entry({ fromQuotedBy: "Bo Halvorsen" }))).toBe("address unknown; quoted by Bo Halvorsen");
  });
});

describe("the names inside a receipt's to: line", () => {
  it("splits the corpus's own format, keeping each name and the text printed for it", () => {
    // The format is the renderer's (see recipientLine in internal/spec/people.go):
    // the names in To, a comma, then `cc` and the names in Cc. A receipt with no
    // recipients at all is an empty string, which is no names rather than one blank
    // one.
    expect(receiptNames("Bo Halvorsen")).toEqual([{ text: "Bo Halvorsen", name: "Bo Halvorsen" }]);
    expect(receiptNames("Maia Balfour, cc Ellen Lindqvist, Lena Whitfield")).toEqual([
      { text: "Maia Balfour", name: "Maia Balfour" },
      { text: "cc Ellen Lindqvist", name: "Ellen Lindqvist" },
      // The marker precedes the group, not each name in it: everything after it is cc,
      // and everything before it is not.
      { text: "Lena Whitfield", name: "Lena Whitfield" },
    ]);
    expect(receiptNames("")).toEqual([]);
  });

  it("keeps the cc marker in the text nothing looks up, and out of the name", () => {
    // The name is what a person is looked up by, and `cc Cy Okafor` is not a name
    // any graph holds; the text is what the line prints, and dropping the marker
    // would restate the sender's own list a different way than the corpus wrote it.
    const [only] = receiptNames("cc Cy Okafor");
    expect(only).toEqual({ text: "cc Cy Okafor", name: "Cy Okafor" });
  });
});

describe("the corpus's identity graph, read once", () => {
  function Probe({ show }: { show: (people: Map<number, string[]>) => ReactNode }) {
    return <>{show(usePersonAddresses())}</>;
  }

  function mount(show: (people: Map<number, string[]>) => ReactNode) {
    const client = makeQueryClient();
    return render(
      <QueryClientProvider client={client}>
        <Probe show={show} />
      </QueryClientProvider>,
    );
  }

  it("answers with the people whose addresses it holds, and only those", async () => {
    vi.stubGlobal("fetch", async () =>
      new Response(
        JSON.stringify({
          people: [
            {
              personId: 2,
              displayName: "Cy Okafor",
              identities: ["display_name:cy okafor", "email:cy@loomworks.example"],
              sent: 3,
              received: 9,
            },
            // A person the corpus knows only by a name: no address, so no key at all
            // rather than a key holding nothing. One absent key is where "we have
            // nothing for them" is said once.
            { personId: 7, displayName: "Ada Okoye", identities: ["display_name:ada okoye"], sent: 1, received: 0 },
            { personId: 9, displayName: "Bo Halvorsen", identities: ["email:bo@fjordline.example"], sent: 2, received: 3 },
          ],
        }),
        { status: 200, headers: { "content-type": "application/json" } },
      ),
    );
    const seen: Map<number, string[]> = new Map();
    mount((people) => {
      seen.clear();
      for (const [id, addresses] of people) seen.set(id, addresses);
      return null;
    });
    await waitFor(() => expect(seen.size).toBe(2));
    expect(seen.get(2)).toEqual(["cy@loomworks.example"]);
    expect(seen.get(9)).toEqual(["bo@fjordline.example"]);
    expect(seen.has(7)).toBe(false);
  });
});
