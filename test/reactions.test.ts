// @vitest-environment jsdom
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import Message from "../src/components/thread/Message";
import Reactions from "../src/components/thread/Reactions";
import Timeline from "../src/components/specs/Timeline";
import { emailFromCorpus, emailFromSpec } from "../src/lib/message/email";
import { normalise } from "../src/lib/timeline/normalise";
import type { Entry } from "../src/lib/timeline/spec";
import { corpus, msg } from "./fixtures";

function markupRoot(html: string) {
  const root = globalThis.document.createElement("div");
  root.innerHTML = html;
  return root;
}

const emojis = ["❤️", "👍🏽", "👩‍💻", "👨‍👩‍👧‍👦", "🇦🇺"];
const reactions: NonNullable<Entry["reactions"]> = emojis.map((emoji, i) => ({
  emoji,
  events: (i === 0 ? [0, 1] : [0]).map((j) => ({
    sourceExtId: `mail:<synthetic-${i}-${j}@example.test>`,
    claimedFrom: "Casey <casey@example.test> <script>untrusted claim</script>",
    date: "Sat, 07 Nov 2026 10:00:00 +0000",
    body: `${emoji}\nSynthetic original notification and quoted target text.`,
    sourceLink: `https://mail.google.com/mail/#all/synthetic-${i}-${j}`,
  })),
}));

describe("received reaction content and native provenance disclosure", () => {
  it("shares exact sequences/events between browser and export email mappings", () => {
    const fromAPI = emailFromCorpus(corpus("mail:<target@example.test>", { reactions }));
    const fromSpec = emailFromSpec(
      msg("target", { reactions }),
      { date: "7 Nov 2026", zone: "stated" },
      (name) => name,
    );
    expect(fromAPI.reactions).toEqual(reactions);
    expect(fromSpec.reactions).toEqual(reactions);
    const html = renderToStaticMarkup(
      createElement(Message, {
        email: { ...fromAPI, body: "<p>Ordinary target content.</p>" },
        place: { id: "target" },
        look: { orgSlot: "unknown" },
      }),
    );
    const document = markupRoot(html);
    expect(document.querySelectorAll(".msg")).toHaveLength(1);
    const chips = document.querySelector('[aria-label="Received reactions"]')!;
    expect(chips.querySelectorAll(":scope > details")).toHaveLength(5);
    for (const [i, chip] of [...chips.children].entries()) {
      expect(chip.querySelector("summary span")?.textContent).toBe(emojis[i]);
      expect(chip.querySelector("summary")?.className).toContain("focus-visible:");
      expect(chip.querySelectorAll("details")).toHaveLength(i === 0 ? 2 : 1);
    }
    expect(chips.querySelector('[aria-label="2 reaction notifications"]')).not.toBeNull();
    expect(chips.querySelectorAll("pre")).toHaveLength(6);
    expect(chips.querySelectorAll("a")).toHaveLength(6);
    expect(chips.textContent).toContain("not verified intent");
    expect(chips.textContent).toContain("mail:<synthetic-0-0@example.test>");
    expect(chips.querySelector("script")).toBeNull();
    expect(html).toContain("&lt;script&gt;");
    // No handlers or hover-only affordance: native summaries work without JS.
    expect(chips.querySelectorAll("button, [onClick], [title]")).toHaveLength(0);
  });

  it("renders the same shared Message chips in a complete script-free timeline", () => {
    const spec = normalise({
      title: "Synthetic received reactions",
      messages: [
        msg("target", {
          sender: "Alex Vale",
          date: "Sat 7 Nov 2026",
          time: "10:00",
          tz: "UTC",
          body: "<p>Ordinary target content.</p>",
          reactions,
        }),
      ],
    });
    const html = renderToStaticMarkup(createElement(Timeline, { spec }));
    const document = markupRoot(html);
    expect(document.querySelectorAll('[aria-label="Received reactions"] > details')).toHaveLength(
      5,
    );
    expect(document.querySelectorAll(".msg")).toHaveLength(1);
    expect(document.querySelectorAll('[aria-label="Received reactions"] pre')).toHaveLength(6);
    for (const emoji of emojis) expect(document.textContent).toContain(emoji);
  });

  it("does not reclassify body-only lookalikes or emit unsafe source links", () => {
    expect(
      emailFromCorpus(corpus("ordinary", { body: "❤️ Casey reacted via Gmail" })).reactions,
    ).toBeUndefined();
    expect(renderToStaticMarkup(createElement(Reactions, {}))).toBe("");
    const unsafe = [
      { emoji: "❤️", events: [{ ...reactions[0]!.events[0]!, sourceLink: "javascript:alert(1)" }] },
    ];
    const document = markupRoot(
      renderToStaticMarkup(createElement(Reactions, { reactions: unsafe })),
    );
    expect(document.querySelector("a")?.hasAttribute("href")).toBe(false);
  });
});

describe("unknown original date projection", () => {
  it("labels host-time placement in browser and static views without changing quoted words", () => {
    const dateNote = "Original date unavailable; placement uses an accessible host's timestamp.";
    const body = "<p>Synthetic bulletin.</p>";
    const apiEmail = emailFromCorpus(
      corpus("quote:undated", { dateNote, html: body, quoted: true }),
    );
    const spec = normalise({
      title: "Undated synthetic quote",
      messages: [msg("undated", { dateNote, body, quoted: true })],
    });
    const browser = markupRoot(
      renderToStaticMarkup(
        createElement(Message, {
          email: apiEmail,
          place: { id: "undated" },
          look: { orgSlot: "unknown" },
        }),
      ),
    );
    const exported = markupRoot(renderToStaticMarkup(createElement(Timeline, { spec })));
    for (const root of [browser, exported]) {
      expect(root.querySelector(".msg summary")?.textContent).toContain(dateNote);
      expect(root.querySelector(".bd")?.textContent).toBe("Synthetic bulletin.");
    }
    expect(apiEmail.body).toBe(body);
  });
});
