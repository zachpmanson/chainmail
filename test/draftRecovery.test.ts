import { expect, it } from "vitest";
import { draftRecoveryText } from "../src/lib/ui/draftRecovery";

it("recovers only editable plaintext, neutralises header newlines, and preserves body exactly", () => {
  const input = {
    to: ["Ada <ada@example.test>", "other@example.test\r\nBcc: injected@example.test"],
    cc: [],
    subject: "First\nSecond",
    body: "<script>not executed</script>\n  own words\n",
    accountId: "private-account",
    id: "opaque-id",
    expectedMessageId: "revision-token",
    credentials: "secret",
  };
  expect(draftRecoveryText(input)).toBe(
    "To: Ada <ada@example.test>, other@example.test Bcc: injected@example.test\nCc: \nSubject: First Second\n\n<script>not executed</script>\n  own words\n",
  );
});
