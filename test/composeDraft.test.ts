import { describe, expect, it } from "vitest";
import { parseAddressHeader } from "../src/components/compose/Draft";

describe("Gmail draft address headers", () => {
  it("restores multiple recipients without splitting commas inside quoted names", () => {
    expect(parseAddressHeader('"Doe, Jane" <jane@example.test>, Bo <bo@example.test>')).toEqual([
      { name: "Doe, Jane", address: "jane@example.test" },
      { name: "Bo", address: "bo@example.test" },
    ]);
  });

  it("treats an absent header as no recipients", () => {
    expect(parseAddressHeader()).toEqual([]);
  });
});
