import { expect, it } from "vitest";
import { fixtureLocation, hasslesFixture } from "./hasslesFixture";

it("rejects unsupported adoption instead of deleting a matching fixture task", () => {
  const fixture = hasslesFixture();
  fixture.handle("/v1/hassles/location", "POST", {
    location: fixtureLocation,
    previous: { accountId: "", calendarId: "" },
    dedicated: true,
  });
  const mutation = {
    location: fixtureLocation,
    action: "create",
    id: "744aa8f9-491b-4473-8dbb-554c957a075c",
    schedule: {
      title: "Keep me",
      start: "2027-01-01T08:30",
      timeZone: "Australia/Melbourne",
      cadence: "daily",
      interval: 1,
      until: "",
    },
  };
  expect(fixture.handle("/v1/hassles", "POST", mutation).status).toBe(200);
  const before = structuredClone(fixture.handle("/v1/hassles"));
  expect(
    fixture.handle("/v1/hassles", "POST", {
      location: fixtureLocation,
      action: "adopt",
      id: `task:${mutation.id}`,
      revision: "1",
      adoptId: mutation.id,
      dryRun: false,
    }).status,
  ).toBe(400);
  expect(fixture.handle("/v1/hassles")).toEqual(before);
  expect(
    fixture.handle("/v1/hassles", "POST", {
      location: fixtureLocation,
      action: "edit",
      id: `task:${mutation.id}`,
      revision: "1",
    }).status,
  ).toBe(400);
  expect(fixture.handle("/v1/hassles")).toEqual(before);
});
