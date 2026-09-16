// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Message, type MessageProps } from "../src/components/Message";

afterEach(cleanup);

/**
 * `Message` is the presentation half of the transcript, so it is tested with no
 * spec anywhere near it: the props below are everything a caller holding a
 * sender, a clock and a body can supply. What only the pipeline can produce —
 * the reply link, the provenance line, an inline edit, the clipboard payload —
 * is furniture the component must draw where it is given and leave out where it
 * is not, and that is what these tests pin.
 */

const bubble = (over: Partial<MessageProps> = {}): MessageProps => ({
  id: "m1",
  body: "<p>invented body</p>",
  sender: "Ada Okoye",
  org: "Loomworks",
  orgSlot: "o2",
  stamp: { date: "Mon 2 Mar 2026", time: "09:15", zone: "unknown" },
  ...over,
});

const draw = (over: Partial<MessageProps> = {}) => render(<Message {...bubble(over)} />);

describe("a bubble drawn from its props alone", () => {
  it("takes the sender, the colour slot and the clock from the caller", () => {
    const { container } = draw();
    const msg = container.querySelector(".msg")!;
    expect(msg.className).toBe("msg o2");
    expect(msg.id).toBe("m1");
    expect(msg.querySelector(".nm")!.textContent).toBe("Ada Okoye");
    expect(msg.querySelector(".org")!.textContent).toBe("Loomworks");
    expect(msg.querySelector(".bd")!.innerHTML).toBe("<p>invented body</p>");
    // the timestamp links to the bubble it is printed on
    expect(msg.querySelector(".tm")!.getAttribute("href")).toBe("#m1");
    expect(msg.querySelector(".to")!.textContent).toBe("to —");
  });

  it("carries the flags it is given as the classes the stylesheet reads", () => {
    const { container } = draw({ me: true, quoted: true, chainStart: true });
    expect(container.querySelector(".msg")!.className).toBe("msg o2 me q chstart");
  });

  it("trims the edges of the body it is handed", () => {
    // A leading blank paragraph is not something the reader should be shown, and
    // the caller should not have to know that: the bubble is where a body's
    // edges become presentable.
    const { container } = draw({ body: "<p> </p><p>Good morning.</p>" });
    expect(container.querySelector(".bd")!.innerHTML).toBe("<p>Good morning.</p>");
  });

  it("stays quiet where the caller has no spec furniture to give it", () => {
    const { container } = draw();
    // nothing only the pipeline can produce — and nothing standing in for it
    expect(container.querySelector(".copyjson")).toBeNull();
    expect(container.querySelector(".newpill, .revpill")).toBeNull();
    expect(container.querySelector(".par, .tstart")).toBeNull();
    expect(container.querySelector(".src")).toBeNull();
    expect(container.querySelector(".edits")).toBeNull();
    expect(container.querySelector(".atts")).toBeNull();
  });

  it("draws the furniture it is given, in the bubble's own places", () => {
    const { container } = draw({
      mark: "new",
      copyJson: { id: "m1" },
      reply: <a className="par">in reply to Bo</a>,
      source: <span className="src">unspooled</span>,
      edits: <div className="edits" />,
    });
    expect(container.querySelector(".msg")!.className).toContain("isnew");
    expect(container.querySelector(".hdr .newpill")!.textContent).toBe("new");
    expect(container.querySelector(".hdr .copyjson")).not.toBeNull();
    expect(container.querySelector(".foot .par")).not.toBeNull();
    expect(container.querySelector(".foot .src")).not.toBeNull();
    expect(container.querySelector(".bub .edits")).not.toBeNull();
  });

  it("copies the payload it was handed, as JSON", () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    draw({ copyJson: { id: "m1", chain: null } });
    fireEvent.click(screen.getByRole("button", { name: /copy this message's json/i }));
    expect(JSON.parse(writeText.mock.calls[0]![0])).toEqual({ id: "m1", chain: null });
  });
});

describe("how a bubble says what it knows about its clock", () => {
  it("states a stated zone plainly", () => {
    const { container } = draw({ stamp: { date: "Mon 2 Mar 2026", time: "09:15", tz: "AEDT", zone: "stated" } });
    const tz = container.querySelector(".tz")!;
    expect(tz.className).toBe("tz");
    expect(tz.textContent).toBe("AEDT");
  });

  it("shows an inferred zone as the claim it is", () => {
    const { container } = draw({ stamp: { date: "Mon 2 Mar 2026", time: "09:15", tz: "AEDT", zone: "inferred" } });
    expect(container.querySelector(".tz")!.className).toBe("tz tzi");
    expect(container.querySelector(".tz")!.textContent).toBe(" AEDT?");
  });

  it("says an unplaced clock is unplaced, rather than leaving it bare", () => {
    const { container } = draw({ stamp: { date: "Mon 2 Mar 2026", zone: "unknown" } });
    expect(container.querySelector(".tz")!.className).toBe("tz tzu");
    expect(container.querySelector(".tz")!.textContent).toBe(" zone unknown");
  });
});
