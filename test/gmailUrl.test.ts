import { describe, expect, it } from "vitest";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { gmailAccountEmail, gmailMessageURL, isGmailMessageURL } from "../src/lib/message/gmailUrl";
import { attHref } from "../src/lib/message/attachments";
import Source from "../src/components/thread/Source";
import Reactions from "../src/components/thread/Reactions";

describe("account-selected Gmail links", () => {
  it("selects only a known mailbox, otherwise keeps the legacy generic link", () => {
    const link = gmailMessageURL("copy-A", "selected+mail@example.test");
    expect(link).toBe(
      "https://mail.google.com/mail/?authuser=selected%2Bmail%40example.test#all/copy-A",
    );
    expect(gmailAccountEmail(link)).toBe("selected+mail@example.test");
    expect(isGmailMessageURL(link)).toBe(true);
    expect(gmailMessageURL("copy-legacy")).toBe("https://mail.google.com/mail/#all/copy-legacy");
    expect(gmailAccountEmail(gmailMessageURL("copy-legacy"))).toBeUndefined();
    expect(
      attHref({
        name: "test.pdf",
        gmailId: "copy-A",
        gmailAccountEmail: "selected+mail@example.test",
      }),
    ).toBe(link);
    expect(
      renderToStaticMarkup(
        createElement(Source, {
          source: "msg copy-A",
          anchorByGmail: new Map(),
          accountEmail: "selected+mail@example.test",
        }),
      ),
    ).toContain(link);
    expect(
      renderToStaticMarkup(
        createElement(Reactions, {
          reactions: [
            {
              emoji: "❤️",
              events: [
                {
                  sourceExtId: "mail:source",
                  claimedFrom: "Synthetic",
                  date: "2026-11-07",
                  body: "exact words",
                  sourceLink: link,
                },
              ],
            },
          ],
        }),
      ),
    ).toContain(link);
  });
  it.each([
    "https://mail.google.com.evil.test/mail/#all/id",
    "https://mail.google.com@mail.evil.test/mail/#all/id",
    "http://mail.google.com/mail/#all/id",
    "https://mail.google.com/mail/?redirect=https://evil.test#all/id",
    "https://mail.google.com/mail/?authuser=A&authuser=B#all/id",
    "https://mail.google.com/mail/?authuser=#all/id",
    "https://mail.google.com/mail/#all/id/extra",
    "javascript:alert(1)",
  ])("refuses noncanonical or unsafe link %s", (link) =>
    expect(isGmailMessageURL(link)).toBe(false),
  );
});
