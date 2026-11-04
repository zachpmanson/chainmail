// @vitest-environment jsdom
import {
  act,
  createElement as h,
  createRef,
  type ReactNode,
  type ButtonHTMLAttributes,
  type RefAttributes,
} from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { expect, it, vi } from "vitest";
import IconButton from "../src/components/ui/IconButton";
import ReceiptIconButton from "../src/components/ui/ReceiptIconButton";
import TooltipProvider from "../src/components/ui/TooltipProvider";
import DialogShell from "../src/components/ui/DialogShell";
import { domHarness, focus, key, settle } from "./domHarness";

const controls: ((
  props: ButtonHTMLAttributes<HTMLButtonElement> & RefAttributes<HTMLButtonElement>,
) => ReactNode)[] = [IconButton, ReceiptIconButton];
const render = domHarness();
const tooltip = () => document.querySelector<HTMLElement>("[data-icon-tooltip]");

it.each(controls)(
  "describes a native icon button on keyboard focus, dismisses with Escape and preserves handlers/ref/style",
  async (Control) => {
    const ref = createRef<HTMLButtonElement>();
    const onFocus = vi.fn();
    const onClick = vi.fn();
    const host = await render(
      h(
        TooltipProvider,
        null,
        h(
          Control,
          {
            ref,
            title: "Reply to this message",
            "aria-label": "Reply",
            className: "text-accent",
            onFocus,
            onClick,
            type: "button",
          },
          "R",
        ),
      ),
    );
    const button = host.querySelector("button")!;
    expect(ref.current).toBe(button);
    expect(host.querySelectorAll("button")).toHaveLength(1);
    expect(button.getAttribute("aria-label")).toBe("Reply");
    expect(button.title).toBe("");
    expect(button.className).toContain("text-accent");
    await key(document.body, "Tab");
    await focus(button);
    expect(onFocus).toHaveBeenCalledOnce();
    expect(tooltip()?.textContent).toBe("Reply to this message");
    expect(host.contains(tooltip())).toBe(false);
    await key(button, "Escape");
    expect(tooltip()).toBeNull();
    await act(async () => button.click());
    expect(onClick).toHaveBeenCalledOnce();
  },
);

it("opens on hover, stays available over its content and closes on Escape", async () => {
  const host = await render(
    h(
      TooltipProvider,
      null,
      h(
        IconButton,
        {
          title: "Archive this thread",
          "aria-label": "Archive",
        },
        "A",
      ),
    ),
  );
  const button = host.querySelector("button")!;
  await act(async () => {
    button.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }));
    button.dispatchEvent(new MouseEvent("mouseenter", { bubbles: false }));
    button.dispatchEvent(new MouseEvent("mousemove", { bubbles: true }));
  });
  await settle(700);
  expect(tooltip()?.textContent).toBe("Archive this thread");
  await act(async () => {
    button.dispatchEvent(new MouseEvent("mouseleave", { relatedTarget: tooltip() }));
    tooltip()!.dispatchEvent(new MouseEvent("mouseenter"));
  });
  await settle();
  expect(tooltip()).not.toBeNull();
  await key(document.body, "Escape");
  expect(tooltip()).toBeNull();
});

it("keeps link attributes and native navigation semantics without a nested button", async () => {
  const ref = createRef<HTMLAnchorElement>();
  const onClick = vi.fn((event) => event.preventDefault());
  const host = await render(
    h(
      TooltipProvider,
      null,
      h(
        IconButton,
        {
          ref,
          href: "/source#message",
          target: "_blank",
          rel: "noopener",
          download: "source.html",
          title: "Read the original source",
          "aria-label": "Source",
          onClick,
        },
        "S",
      ),
    ),
  );
  const link = host.querySelector("a")!;
  expect(ref.current).toBe(link);
  expect(host.querySelector("button")).toBeNull();
  expect(link.getAttribute("href")).toBe("/source#message");
  expect(link.target).toBe("_blank");
  expect(link.rel).toBe("noopener");
  expect(link.download).toBe("source.html");
  expect(link.hasAttribute("role")).toBe(false);
  await key(document.body, "Tab");
  await focus(link);
  expect(tooltip()?.textContent).toBe("Read the original source");
  await act(async () => link.click());
  expect(onClick).toHaveBeenCalledOnce();
});

it.each(controls)(
  "retains native disabled behaviour without a focusable wrapper",
  async (Control) => {
    const onClick = vi.fn();
    const host = await render(
      h(
        TooltipProvider,
        null,
        h(
          Control,
          {
            disabled: true,
            title: "Archive",
            "aria-label": "Archive",
            onClick,
          },
          "A",
        ),
      ),
    );
    const button = host.querySelector("button")!;
    expect(button.disabled).toBe(true);
    expect(button.title).toBe("Archive");
    await focus(button);
    await act(async () => {
      button.click();
      button.dispatchEvent(new MouseEvent("mouseenter"));
    });
    await settle(650);
    expect(document.activeElement).not.toBe(button);
    expect(tooltip()).toBeNull();
    expect(onClick).not.toHaveBeenCalled();
    expect(host.querySelectorAll("[tabindex]")).toHaveLength(0);
  },
);

it("leaves untitled controls alone and keeps native hints in non-hydrated static exports", async () => {
  const host = await render(
    h(TooltipProvider, null, h(IconButton, { "aria-label": "Reply" }, "R")),
  );
  await focus(host.querySelector("button")!);
  expect(tooltip()).toBeNull();
  const html = renderToStaticMarkup(
    h(
      "div",
      null,
      h(IconButton, { href: "#source", title: "Source details", "aria-label": "Source" }, "S"),
      h(ReceiptIconButton, { title: "Reply details", "aria-label": "Reply", disabled: true }, "R"),
    ),
  );
  const staticHost = document.createElement("div");
  staticHost.innerHTML = html;
  expect(staticHost.querySelector("a")?.title).toBe("Source details");
  expect(staticHost.querySelector("a")?.getAttribute("href")).toBe("#source");
  expect(staticHost.querySelector("button")?.title).toBe("Reply details");
  expect(staticHost.querySelector("button")?.disabled).toBe(true);
  expect(html).not.toContain("tooltip");
});

it("consumes the first Escape in a dialog without closing the dialog", async () => {
  const onClose = vi.fn();
  await render(
    h(
      TooltipProvider,
      null,
      h(DialogShell, {
        label: "Details",
        onClose,
        initialFocus: false,
        children: h(IconButton, { title: "Read source", "aria-label": "Source" }, "S"),
      }),
    ),
  );
  const button = document.querySelector<HTMLButtonElement>("[aria-label=Source]")!;
  await key(document.body, "Tab");
  await focus(button);
  expect(tooltip()).not.toBeNull();
  await key(button, "Escape");
  expect(tooltip()).toBeNull();
  expect(onClose).not.toHaveBeenCalled();
  expect(document.querySelector("[role=dialog]")).not.toBeNull();
  await key(button, "Escape");
  expect(onClose).toHaveBeenCalledOnce();
});
