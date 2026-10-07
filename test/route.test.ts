import { describe, expect, it } from "vitest";
import { slug, untitledName } from "../src/lib/inbox/route";

describe("slug", () => {
  it("lowercases and hyphenates, trimming the edges", () => {
    expect(slug("  Daystrom: Telemetry CSV!  ")).toBe("daystrom-telemetry-csv");
  });

  it("is empty for a title with nothing usable", () => {
    expect(slug("—!—")).toBe("");
  });

  it("caps the name at 64 characters", () => {
    expect(slug("a".repeat(80))).toHaveLength(64);
  });
});

describe("untitledName", () => {
  it("is a spec- name in base 36", () => {
    expect(untitledName()).toMatch(/^spec-[0-9a-z]+$/);
  });
});
