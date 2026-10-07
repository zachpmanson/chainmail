// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ComposerFlow } from "../src/components/ComposerFlow";

afterEach(cleanup);

describe("the compose panel", () => {
  it("is a right-side panel rather than a modal or backdrop", () => {
    render(
      <ComposerFlow
        variant="compose"
        step="compose"
        busy={false}
        editor={<p>Message editor</p>}
        preview={null}
        onReview={() => {}}
        onEdit={() => {}}
        onConfirm={() => {}}
        onClose={() => {}}
      />,
    );

    expect(
      screen
        .getByRole("complementary", { name: "Compose email" })
        .classList.contains("compose-panel"),
    ).toBe(true);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.querySelector(".compose-backdrop")).toBeNull();
  });
});
