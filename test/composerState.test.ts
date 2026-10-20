import { describe, expect, it } from "vitest";
import DraftOwnership from "../src/lib/message/draftOwnership";
import ReviewInput from "../src/lib/message/reviewedInput";

describe("review generations", () => {
  it("copies input arrays rather than holding editor aliases", () => {
    const reviews = new ReviewInput<{ to: string[] }>();
    const to = ["reviewed@example.test"];
    reviews.update({ to });
    const reviewed = reviews.capture();
    to.push("not-reviewed@example.test");
    expect(reviewed.input.to).toEqual(["reviewed@example.test"]);
    reviews.update({ to });
    expect(reviews.isCurrent(reviewed)).toBe(false);
  });

  it("rejects old responses even after returning to identical inputs", () => {
    const reviews = new ReviewInput<string>();
    reviews.update("A");
    const a = reviews.capture();
    reviews.update("B");
    reviews.update("A");
    expect(reviews.isCurrent(a)).toBe(false);
    const first = reviews.capture();
    const latest = reviews.capture();
    expect(reviews.isCurrent(first)).toBe(false);
    expect(reviews.isCurrent(latest)).toBe(true);
  });
});

describe("draft ownership", () => {
  it("preserves dirty ownership through list responses, but scopes it to the editor", () => {
    const owner = new DraftOwnership();
    owner.enter("account/thread", false);
    expect(owner.canHydrate()).toBe(true);
    owner.edit();
    owner.enter("account/thread", false);
    expect(owner.canHydrate()).toBe(false);
    owner.enter("account/other-thread", true);
    expect(owner.canHydrate()).toBe(false);
    owner.enter("account/empty-thread", false);
    expect(owner.canHydrate()).toBe(true);
  });

  it("retires account-local IDs without releasing a different dirty editor", () => {
    const owner = new DraftOwnership();
    owner.enter("acct/A", false);
    owner.enter("acct/B", true);
    owner.retire("acct", "d1");
    expect(owner.canHydrate()).toBe(false);
    expect(owner.isRetired("acct", "d1")).toBe(true);
    expect(owner.isRetired("other-account", "d1")).toBe(false);
    owner.enter("acct/A", false);
    expect(owner.isRetired("acct", "d1")).toBe(true);
  });
});
