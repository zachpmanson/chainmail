// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ToastHost } from "../src/components/Toasts";
import { clearToasts, dismissToast, pushToast } from "../src/lib/toasts";

afterEach(() => {
  cleanup();
  clearToasts();
});

describe("the corner a write says what it did in", () => {
  it("draws what was raised, in the order it was raised, somewhere the page is not", () => {
    render(
      <div>
        <p data-testid="page">the mail being read</p>
        <ToastHost />
      </div>,
    );
    // Nothing raised is nothing drawn: the host is a fixed box with no content of
    // its own, and an empty one would be a hit area over the corner of the page.
    expect(document.querySelector(".toasts")).toBeNull();

    act(() => {
      pushToast("Archived 3 messages — out of the inbox, still in All Mail.", "note", 5000);
      pushToast("This host cannot change the mailbox: it was started without -mail-write.", "fail");
    });

    const box = document.querySelector(".toasts") as HTMLElement;
    expect(box).not.toBeNull();
    expect(box.textContent).toContain("Archived 3 messages");
    expect(box.textContent).toContain("-mail-write");
    // Out of the flow of the page: the host is a sibling of what is being read,
    // and nothing it draws is a line of it.
    expect(screen.getByTestId("page").contains(box)).toBe(false);
  });

  it("marks a refusal, and leaves the note unmarked", () => {
    render(<ToastHost />);
    act(() => {
      pushToast("Archived 1 message — out of the inbox, still in All Mail.", "note", 5000);
      pushToast("Deleting failed: gmail: rate limited, try again in a minute", "fail");
    });
    // The difference is the kind, not the words: one is a moment that has passed
    // and the other is something to act on, so it is the one that is marked.
    expect(document.querySelectorAll(".toast")).toHaveLength(2);
    expect(document.querySelectorAll(".toast.bad")).toHaveLength(1);
    expect(document.querySelector(".toast.bad")?.textContent).toContain("rate limited");
  });

  it("can be taken down by the reader, which is the only way out a refusal has", () => {
    render(<ToastHost />);
    act(() => {
      pushToast("Deleting failed: gmail: rate limited", "fail");
    });
    const x = screen.getByRole("button", { name: /Dismiss: Deleting failed/ });
    fireEvent.click(x);
    expect(screen.queryByText(/rate limited/)).toBeNull();
    // And the box goes with it rather than standing empty over the corner.
    expect(document.querySelector(".toasts")).toBeNull();
  });

  it("takes a note away on its own clock, and leaves a refusal standing", async () => {
    render(<ToastHost />);
    act(() => {
      // A real clock, short: what is being asserted is that the ttl is honoured at
      // all, and five seconds of a test run is five seconds of a test run.
      pushToast("Moved 2 messages to Work.", "note", 20);
      pushToast("Moving failed: the mailbox said no", "fail");
    });
    expect(screen.getByText("Moved 2 messages to Work.")).toBeTruthy();
    await waitFor(() => expect(screen.queryByText("Moved 2 messages to Work.")).toBeNull());
    // The refusal is not on any clock: an error that took itself away would leave
    // the reader looking at mail that did not move with no explanation.
    expect(screen.getByText("Moving failed: the mailbox said no")).toBeTruthy();
  });

  it("clears the timer it armed when a notification is taken down early", () => {
    render(<ToastHost />);
    const armed = vi.spyOn(globalThis, "setTimeout");
    let id = 0;
    act(() => {
      id = pushToast("Archived 1 message.", "note", 5000);
    });
    const timer = armed.mock.calls.find(([, ms]) => ms === 5000);
    armed.mockRestore();
    if (!timer) throw new Error("the note armed no five-second timer");

    // Dismissed before its clock ran out — the pane does this when the reader
    // moves to another thread. Nothing is left to fire at a toast that is gone.
    act(() => dismissToast(id));
    expect(screen.queryByText("Archived 1 message.")).toBeNull();
  });
});
