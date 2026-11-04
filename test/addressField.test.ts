// @vitest-environment jsdom
import { act, createElement as h, useState } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { expect, it, vi } from "vitest";
import AddressField, {
  addressKey,
  addressWords,
  typedAddress,
  type Address,
} from "../src/components/compose/AddressField";
import { domHarness, focus, key, pick, settle, type } from "./domHarness";
import RecipientRows from "../src/components/compose/RecipientRows";
import { emptyDraft } from "../src/components/compose/Draft";

const render = domHarness();
const suggestions: Address[] = [
  { name: "Alice", address: "alice@example.test" },
  { name: "Alina", address: "alina@example.test" },
  { name: "Bob", address: "bob@example.test" },
  ...Array.from({ length: 10 }, (_, i) => ({
    name: `Extra ${i}`,
    address: `extra${i}@example.test`,
  })),
];
const input = (label = "to") =>
  document.querySelector<HTMLInputElement>(`[aria-label='${label} addresses']`)!;
const options = () => [...document.querySelectorAll<HTMLElement>("[role=option]")];
const status = () =>
  [...document.querySelectorAll("[role=status]")].map((n) => n.textContent).join(" ");
async function harness({
  initial = [],
  taken = [],
  mine = [],
  disabled = false,
}: {
  initial?: Address[];
  taken?: Address[];
  mine?: string[];
  disabled?: boolean;
} = {}) {
  const change = vi.fn();
  const submit = vi.fn();
  const escape = vi.fn();
  function Harness() {
    const [value, setValue] = useState(initial);
    return h(
      "form",
      {
        onSubmit: (e) => {
          e.preventDefault();
          submit();
        },
        onKeyDown: (e) => {
          if (e.key === "Escape") escape();
        },
      },
      h(AddressField, {
        label: "to",
        value,
        suggestions,
        taken,
        mine,
        disabled,
        onChange: (next) => {
          setValue(next);
          change(next);
        },
      }),
      h("button", { type: "submit" }, "Send"),
    );
  }
  const host = await render(h(Harness));
  await focus(input());
  return { host, change, submit, escape };
}

it("preserves shallow parsing and exported address helpers", () => {
  expect(typedAddress('  "Alice A" <Alice@Example.test>  ')).toEqual({
    name: "Alice A",
    address: "Alice@Example.test",
  });
  expect(typedAddress("custom@local")).toEqual({ address: "custom@local" });
  for (const text of ["invalid", "a @b", "a@b;c@d", "a@b,c@d"])
    expect(typedAddress(text)).toBeNull();
  expect(addressKey(" Alice@Example.test ")).toBe("alice@example.test");
  expect(addressWords({ name: "Alice", address: "alice@test" })).toBe("Alice <alice@test>");
});

it("caps ordered suggestions, portals them, and accepts keyboard-highlighted names once per delimiter", async () => {
  const { host, change, submit } = await harness();
  expect(options()).toHaveLength(0);
  await type(input(), "e");
  expect(options()).toHaveLength(8);
  expect(options()[0]!.textContent).toBe("Alicealice@example.test");
  expect(host.contains(options()[0]!)).toBe(false);
  expect(document.getElementById(input().getAttribute("aria-controls")!)).toBeTruthy();
  await type(input(), "ali");
  expect(options()).toHaveLength(2);
  await key(input(), "ArrowDown");
  await key(input(), "ArrowUp");
  await key(input(), "ArrowDown");
  expect(
    document.getElementById(input().getAttribute("aria-activedescendant")!)?.textContent,
  ).toContain("Alina");
  expect((await key(input(), "Enter")).defaultPrevented).toBe(true);
  expect(change).toHaveBeenCalledExactlyOnceWith([suggestions[1]]);
  expect(input().value).toBe("");
  expect(input().getAttribute("aria-expanded")).toBe("false");
  await type(input(), "ali");
  await key(input(), ",");
  expect(change).toHaveBeenLastCalledWith([suggestions[1], suggestions[0]]);
  await type(input(), "bob");
  await key(input(), ";");
  expect(change).toHaveBeenLastCalledWith([suggestions[1], suggestions[0], suggestions[2]]);
  expect(change).toHaveBeenCalledTimes(3);
  expect(submit).not.toHaveBeenCalled();
});

it("accepts pointer and free-form addresses, preserving names/order and input focus", async () => {
  const { change, submit } = await harness();
  await type(input(), "ali");
  await pick(options()[1]!);
  expect(change).toHaveBeenCalledExactlyOnceWith([suggestions[1]]);
  expect(document.activeElement).toBe(input());
  await type(input(), '"New Person" <Custom@elsewhere>');
  await key(input(), ";");
  expect(change).toHaveBeenLastCalledWith([
    suggestions[1],
    { name: "New Person", address: "Custom@elsewhere" },
  ]);
  await type(input(), "bare@somewhere");
  await key(input(), "Enter");
  expect(change).toHaveBeenCalledTimes(3);
  expect(change.mock.lastCall![0][2]).toEqual({ address: "bare@somewhere" });
  expect(submit).not.toHaveBeenCalled();
});

it("excludes and refuses own/taken/current addresses case-insensitively, but only treats supplied mine as own", async () => {
  const { change } = await harness({
    initial: [{ name: "Original Alice", address: "ALICE@example.test" }],
    taken: [{ address: "ALINA@EXAMPLE.TEST" }],
    mine: [" BOB@EXAMPLE.TEST "],
  });
  await type(input(), "example");
  expect(options().every((row) => row.textContent?.startsWith("Extra"))).toBe(true);
  for (const [text, message] of [
    ["alice@EXAMPLE.test", "already on this reply"],
    ["alina@EXAMPLE.test", "other list"],
    ["bob@EXAMPLE.test", "your own address"],
  ]) {
    await type(input(), text!);
    await key(input(), "Enter");
    expect(status()).toContain(message);
    expect(change).not.toHaveBeenCalled();
  }
  await key(input(), "Escape");
  expect(status()).toBe("");
  expect(input().value).toBe("");
  // No implicit account-address policy: without mine, Bob remains a normal recipient.
  await harness();
  await type(input(), "bob");
  await key(input(), "Enter");
  expect(document.querySelector("[data-list=to]")?.textContent).toContain("Bob <bob@example.test>");
});

it("keeps invalid input feedback, blocks form acceptance, and Escape clears query/refusal without escaping to the parent", async () => {
  const { change, submit, escape } = await harness();
  await type(input(), "not an address");
  expect(status()).toContain("is not an address");
  expect((await key(input(), "Enter")).defaultPrevented).toBe(true);
  expect(change).not.toHaveBeenCalled();
  expect(submit).not.toHaveBeenCalled();
  await key(input(), "Escape");
  expect(escape).not.toHaveBeenCalled();
  expect(input().value).toBe("");
  expect(input().getAttribute("aria-expanded")).toBe("false");
  await key(input(), "Escape");
  expect(escape).toHaveBeenCalledOnce();
});

it("removes only the last chip on empty Backspace and individual chips by their non-submit controls", async () => {
  const { change, host, submit } = await harness({ initial: suggestions.slice(0, 3) });
  await type(input(), "x");
  await key(input(), "Backspace");
  expect(change).not.toHaveBeenCalled();
  await type(input(), "");
  await key(input(), "Backspace");
  expect(change).toHaveBeenCalledExactlyOnceWith(suggestions.slice(0, 2));
  const remove = host.querySelector<HTMLButtonElement>(
    "[aria-label='remove Alice <alice@example.test>']",
  )!;
  expect(remove.type).toBe("button");
  await pick(remove);
  expect(change).toHaveBeenLastCalledWith([suggestions[1]]);
  expect(submit).not.toHaveBeenCalled();
});

it("dismisses on outside focus and reopens without losing the typed query", async () => {
  const { host } = await harness();
  await type(input(), "ali");
  await focus(host.querySelector<HTMLButtonElement>("[type=submit]")!);
  expect(input().getAttribute("aria-expanded")).toBe("false");
  expect(input().value).toBe("ali");
  await focus(input());
  expect(options()).toHaveLength(2);
});

it("disables the native input/chip buttons and closes an already open list", async () => {
  const { change, host } = await harness({ initial: [suggestions[0]!], disabled: true });
  expect(input().disabled).toBe(true);
  const remove = host.querySelector<HTMLButtonElement>("[aria-label^=remove]")!;
  expect(remove.disabled).toBe(true);
  await act(async () => remove.click());
  await key(input(), "Backspace");
  expect(change).not.toHaveBeenCalled();
  expect(options()).toHaveLength(0);
  const props = { label: "to", value: [], onChange: change, suggestions };
  await render(h(AddressField, props));
  await focus(input());
  await type(input(), "ali");
  expect(options()).toHaveLength(2);
  await render(h(AddressField, { ...props, disabled: true }));
  expect(options()).toHaveLength(0);
});

it("generates unique list and option IDs even for repeated labels, and renders recipient chips on the server", async () => {
  const props = { label: "to", value: [suggestions[2]!], suggestions, onChange: vi.fn() };
  await render(h("div", null, h(AddressField, props), h(AddressField, props)));
  const inputs = [...document.querySelectorAll<HTMLInputElement>("[role=combobox]")];
  const ids: string[] = [];
  for (const field of inputs) {
    await focus(field);
    await type(field, "ali");
    ids.push(field.getAttribute("aria-controls")!, field.getAttribute("aria-activedescendant")!);
    expect(document.getElementById(ids.at(-2)!)).toBeTruthy();
    expect(document.getElementById(ids.at(-1)!)).toBeTruthy();
  }
  expect(new Set(ids).size).toBe(4);
  const html = renderToStaticMarkup(h(AddressField, props));
  expect(html).toContain("Bob &lt;bob@example.test&gt;");
  expect(html).toContain('role="combobox"');
  expect(html).not.toContain('role="listbox"');
  await settle();
});

it("keeps To/Cc exclusions live through RecipientRows and never implicitly submits the real enclosing form", async () => {
  const submit = vi.fn();
  function Harness() {
    const [draft, setDraft] = useState(emptyDraft);
    return h(
      "form",
      {
        onSubmit: (e) => {
          e.preventDefault();
          submit();
        },
      },
      h(RecipientRows, {
        draft,
        onDraft: (patch) => setDraft((old) => ({ ...old, ...patch })),
        from: "sender@example.test",
        suggestions,
        mine: [],
        busy: false,
      }),
      h("button", { type: "submit" }, "Send"),
    );
  }
  const host = await render(h(Harness));
  await focus(input("to"));
  await type(input("to"), "alice");
  expect((await key(input("to"), "Enter")).defaultPrevented).toBe(true);
  await focus(input("cc"));
  await type(input("cc"), "alice");
  expect(options()).toHaveLength(0);
  await type(input("cc"), "ALICE@EXAMPLE.TEST");
  await key(input("cc"), "Enter");
  expect(status()).toContain("other list");
  await key(input("cc"), "Escape");
  await pick(
    host.querySelector<HTMLButtonElement>("[aria-label='remove Alice <alice@example.test>']")!,
  );
  await focus(input("cc"));
  await type(input("cc"), "alice");
  await pick(options()[0]!);
  expect(host.querySelector("[data-list=to]")!.textContent).not.toContain("Alice");
  expect(host.querySelector("[data-list=cc]")!.textContent).toContain("Alice <alice@example.test>");
  expect(submit).not.toHaveBeenCalled();
  await act(async () => host.querySelector<HTMLButtonElement>("[type=submit]")!.click());
  expect(submit).toHaveBeenCalledOnce();
});

it("does not accept or suppress native text composition Enter", async () => {
  const { change } = await harness();
  await type(input(), "alice");
  const event = new KeyboardEvent("keydown", {
    key: "Enter",
    bubbles: true,
    cancelable: true,
    isComposing: true,
  });
  await act(async () => input().dispatchEvent(event));
  await settle();
  expect(event.defaultPrevented).toBe(false);
  expect(change).not.toHaveBeenCalled();
  expect(input().value).toBe("alice");
});
