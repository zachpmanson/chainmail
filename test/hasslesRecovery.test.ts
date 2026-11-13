import { expect, it } from "vitest";
import { parseRecovery, ready, settled } from "../src/lib/hassles/recovery";
const epoch = "11111111-1111-4111-8111-111111111111";
const f = (n: string | number) => `${epoch}:${n}`;
const pending = {
  operationId: "22222222-2222-4222-8222-222222222222",
  action: "delete",
  title: "Original",
  components: 24,
  location: { accountId: "offline", calendarId: "/caldav/v2/offline@example.test/events/" },
};
it.each([false, 0, "", null, [], {}, undefined])(
  "rejects present malformed pending %j even on ready",
  (value) => {
    expect(ready({ status: "ready", fence: f(1), pending: value })).toBe(false);
  },
);
it.each([
  "",
  "fence-1",
  `${epoch}:01`,
  `${epoch}:-1`,
  `${epoch}:1.0`,
  `${epoch}:18446744073709551616`,
  "INVALID:1",
  "11111111-1111-0111-8111-111111111111:1",
  "11111111-1111-4111-7111-111111111111:1",
])("rejects malformed fence %s", (value) => {
  expect(ready({ status: "ready", fence: value })).toBe(false);
});
it.each([
  null,
  [],
  {},
  { status: "future" },
  { status: "ready" },
  { status: "pending", pending },
  { status: "pending", fence: f(1), pending: { ...pending, operationId: "server-original" } },
  { status: "pending", fence: f(1), pending: { ...pending, components: 25 } },
  { status: "pending", fence: f(1), pending: { ...pending, components: 0 } },
  { status: "pending", fence: f(1), pending: { ...pending, location: [] } },
  { status: "busy", pending: null },
])("fails closed for malformed recovery %j", (value) =>
  expect(parseRecovery(value)).toBeUndefined(),
);
it("accepts canonical statuses and clones public metadata", () => {
  const source = { status: "pending", fence: f(1), pending: structuredClone(pending) };
  const parsed = parseRecovery(source)!;
  source.pending.location.accountId = "changed";
  expect(parsed.pending?.location.accountId).toBe("offline");
  expect(ready({ status: "ready", fence: f(0) })).toBe(true);
  expect(ready({ status: "ready", fence: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa:1" })).toBe(true);
  expect(ready({ status: "ready", fence: "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA:1" })).toBe(false);
});
it.each([f(0), f(1), f(3), "22222222-2222-4222-8222-222222222222:2", "bad"])(
  "rejects non-exact settlement fence %s",
  (fence) =>
    expect(settled({ settled: true, recovery: { status: "ready", fence } }, f(1))).toBe(false),
);
it("uses exact uint64 arithmetic beyond Number precision", () => {
  const result = (n: string) => ({ settled: true, recovery: { status: "ready", fence: f(n) } });
  expect(settled(result("9007199254740993"), f("9007199254740992"))).toBe(true);
  expect(settled(result("9007199254740994"), f("9007199254740992"))).toBe(false);
  expect(settled(result("18446744073709551615"), f("18446744073709551614"))).toBe(true);
  expect(settled(result("0"), f("18446744073709551615"))).toBe(false);
});
