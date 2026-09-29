// @vitest-environment jsdom
import { useState } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { AddressField, type Address } from "../src/components/AddressField";

/**
 * The address field on its own: what it offers, what it takes, and what it refuses.
 *
 * The reply box's own tests cover the field in the sentence it is drawn in (see
 * replyBox.test.tsx); what is checked here is the control's own behaviour, where the
 * failures are quiet ones — a suggestion that cannot be picked, an address that was
 * typed and silently dropped, an address that ends up on the reply twice.
 *
 * Everything below is invented; this corpus holds real correspondence.
 */

afterEach(cleanup);

const ADA = { name: "Ada Okoye", address: "ada@loomworks.example" };
const CY = { name: "Cy Okafor", address: "cy@loomworks.example" };
const CARL = { address: "carl@example.net" };
const SUGGESTIONS = [ADA, CY, CARL];

/** The reader's own addresses, which a reply is not sent to. */
const MINE = ["marit@loomworks.example"];

/**
 * The two fields as the reply box arranges them: one answer about who is on the
 * reply, in two lists, each refusing what the other holds. A harness rather than
 * the component alone, because an address may only be named once across the pair.
 */
function Reply({ start = [] as Address[], other = [] as Address[] }) {
  const [to, setTo] = useState<Address[]>(start);
  const [cc, setCc] = useState<Address[]>(other);
  const shared = {
    suggestions: SUGGESTIONS,
    mine: MINE,
  };
  return (
    <div>
      <AddressField
        {...shared}
        label="to"
        value={to}
        onChange={setTo}
        taken={cc}
      />
      <AddressField
        {...shared}
        label="cc"
        value={cc}
        onChange={setCc}
        taken={to}
      />
    </div>
  );
}

const field = (list: "to" | "cc") => screen.getByLabelText(`${list} addresses`) as HTMLInputElement;
const type = (list: "to" | "cc", text: string) =>
  fireEvent.change(field(list), { target: { value: text } });
const kind = (list: "to" | "cc", key: string) => fireEvent.keyDown(field(list), { key });
/** The addresses one list holds, as its chips print them. */
const chips = (list: "to" | "cc") =>
  [...document.querySelectorAll<HTMLElement>(`.addrfield[data-list="${list}"] .addrname`)].map(
    (c) => c.textContent!,
  );
/** What the field is offering, in the order it offers it — the rows a reader picks
 *  from, and the one the reader is on. */
const offered = () =>
  [...document.querySelectorAll<HTMLElement>(".addropt")].map((o) => o.textContent!);
const chipOf = (list: "to" | "cc", address: string) =>
  [...document.querySelectorAll<HTMLElement>(`.addrfield[data-list="${list}"] .addrchip`)].find((c) =>
    c.querySelector(".addrname")!.textContent!.includes(address),
  )!;
const refusal = () => document.querySelector(".addrrefuse")?.textContent ?? null;

describe("the field that builds a list of addresses", () => {
  it("offers the addresses it was given, and filters them as the reader types", () => {
    render(<Reply />);
    // Nothing is offered before anything is typed: the list is an answer to what
    // the reader is halfway through, and a field that dropped its whole corpus open
    // on focus would be a menu over the sentence they are writing.
    expect(offered()).toEqual([]);

    type("to", "loom");
    expect(offered()).toEqual(["Ada Okoyeada@loomworks.example", "Cy Okaforcy@loomworks.example"]);

    // Both halves of what a reader half-remembers about a person find them: the
    // name, and the address.
    type("to", "okafor");
    expect(offered()).toEqual(["Cy Okaforcy@loomworks.example"]);
    type("to", "example.net");
    expect(offered()).toEqual(["carl@example.net"]);
  });

  it("takes an address the corpus has never seen, which is the point of the field", () => {
    render(<Reply />);

    type("to", "stranger@example.org");
    // What they typed is offered as a row of its own, beside the suggestions: an
    // address nobody knows is a thing this field can hold, and a reader should be
    // able to see that before they press anything.
    expect(offered()).toEqual(["stranger@example.org"]);
    kind("to", "Enter");

    expect(chips("to")).toEqual(["stranger@example.org"]);
    expect(field("to").value).toBe("");
    expect(refusal()).toBeNull();
  });

  it("reads a name and an address typed together, and nothing that is not an address", () => {
    render(<Reply />);

    // A name pasted out of a message is not something the reader should have to
    // strip: the address is what it is sent to, and the name is what the chip
    // prints beside it.
    type("to", "Ada Okoye <ada@example.org>");
    kind("to", "Enter");
    expect(chips("to")).toEqual(["Ada Okoye <ada@example.org>"]);

    type("cc", "the fitters");
    kind("cc", "Enter");
    expect(chips("cc")).toEqual([]);
    expect(offered()).toEqual([
      "the fitters is not an address — an address has something@somewhere in it.",
    ]);
    // And the text is left where it is: the reader is being told what to fix rather
    // than being made to type it again.
    expect(field("cc").value).toBe("the fitters");
  });

  it("refuses the reader's own address, and says so", () => {
    render(<Reply />);

    type("to", "marit@loomworks.example");
    // Not offered as a suggestion either, so the field is not asking the reader to
    // pick the one thing it would refuse.
    expect(offered()).toEqual(["marit@loomworks.example"]);
    kind("to", "Enter");

    expect(chips("to")).toEqual([]);
    expect(refusal()).toContain("your own address");
  });

  it("refuses an address that is already on the reply, in either list", () => {
    render(<Reply start={[ADA]} other={[CARL]} />);

    // In this list.
    type("to", "ada");
    expect(offered()).not.toContain("Ada Okoyeada@loomworks.example");
    type("to", "ada@loomworks.example");
    kind("to", "Enter");
    expect(chips("to")).toEqual(["Ada Okoye <ada@loomworks.example>"]);
    expect(refusal()).toContain("already on this reply");

    // And in the other one, which is the same address twice: one recipient is one
    // address in one list, so it must be removed there before it can be added here.
    type("to", "carl@example.net");
    kind("to", "Enter");
    expect(chips("to")).toEqual(["Ada Okoye <ada@loomworks.example>"]);
    expect(refusal()).toContain("in the other list");
  });

  it("takes an address off the list when its chip's press is taken", () => {
    render(<Reply start={[CY, ADA]} />);

    fireEvent.click(within(chipOf("to", "cy@loomworks.example")).getByRole("button", { name: /^remove / }));
    expect(chips("to")).toEqual(["Ada Okoye <ada@loomworks.example>"]);
  });

  it("has only a remove control on each chip; an address can be re-added in the other list", () => {
    render(<Reply start={[ADA, CY]} />);

    const cyChip = chipOf("to", "cy@loomworks.example");
    expect(within(cyChip).getAllByRole("button")).toHaveLength(1);
    fireEvent.click(within(cyChip).getByRole("button", { name: /^remove / }));
    type("cc", "cy");
    fireEvent.click(screen.getByRole("option", { name: /Cy Okafor/ }));

    expect(chips("to")).toEqual(["Ada Okoye <ada@loomworks.example>"]);
    expect(chips("cc")).toEqual(["Cy Okafor <cy@loomworks.example>"]);
  });

  it("picks the row the reader has arrowed to, and the last chip answers backspace", () => {
    render(<Reply start={[ADA]} />);

    // Enter takes the row the reader is on rather than the top of the list: a
    // reader who has arrowed down has said which address they mean.
    type("to", "loom");
    kind("to", "ArrowDown");
    kind("to", "Enter");
    expect(chips("to")).toEqual(["Ada Okoye <ada@loomworks.example>", "Cy Okafor <cy@loomworks.example>"]);

    // And a field that builds a list has to unbuild it without the mouse: the
    // backspace in an empty input is the chip the caret is against.
    kind("to", "Backspace");
    expect(chips("to")).toEqual(["Ada Okoye <ada@loomworks.example>"]);
    // While there is text in the field, backspace is the reader editing that text.
    type("to", "ad");
    kind("to", "Backspace");
    expect(chips("to")).toEqual(["Ada Okoye <ada@loomworks.example>"]);
  });
});
