// @vitest-environment jsdom
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ReplyLink } from "../src/components/ReplyLink";

/**
 * The mark both renderers draw for one message's parent: a built page resolves
 * the parent through the spec's rows, the reading pane through the corpus entries
 * it was handed, and both hand the result here. What this component owns is the
 * markup and the words — the arrow, the label, the hover, and what a message with
 * no parent says instead.
 */
describe("the reply link", () => {
  it("points at the parent and names it", () => {
    const { container } = render(
      <ReplyLink parent={{ anchor: "entry-3", who: "Ada Okoye", when: "Mon, 2 Mar 2026 19:15" }} />,
    );
    const par = container.querySelector(".par")!;
    expect(par.getAttribute("href")).toBe("#entry-3");
    expect(par.querySelector(".parlbl")!.textContent).toBe(
      "in reply to Ada Okoye, Mon, 2 Mar 2026 19:15",
    );
    // The title is the whole sentence: in column mode the stylesheet collapses the
    // label to the arrow, so the hover is where the words still are.
    expect(par.getAttribute("title")).toBe("In reply to Ada Okoye, Mon, 2 Mar 2026 19:15");
    expect(par.querySelector(".arw")).not.toBeNull();
  });

  it("says a message opens the thread when it answers nothing", () => {
    const { container } = render(<ReplyLink parent={null} />);
    expect(container.querySelector(".tstart")!.textContent).toBe("thread start");
    expect(container.querySelector(".par")).toBeNull();
  });
});
