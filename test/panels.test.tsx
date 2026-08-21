import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { renderToStaticMarkup } from "react-dom/server";
import { SourcesPanel } from "../src/components/Panels";
import { Timeline } from "../src/components/Timeline";
import { derive } from "../src/lib/derive";
import { normalise } from "../src/lib/normalise";

const spec = normalise(JSON.parse(readFileSync("fixtures/synthetic.json", "utf8")));

function chainRows() {
  const v = derive(spec);
  return {
    v,
    chains: v.layout.chains.map((c) => ({
      root: c.root,
      subject: c.subject,
      opener: c.opener,
      date: c.date,
      count: c.entries.length,
      anchor: c.root,
      gmailId: c.entries
        .map((id) => v.rows.find((r) => r.id === id)!)
        .find((r) => r.entry.threadId ?? r.entry.gmailId)?.entry.gmailId,
    })),
  };
}

describe("sources panel", () => {
  it("renders a checkbox per chain, tagged with the chain root", () => {
    const { v, chains } = chainRows();
    const html = renderToStaticMarkup(
      <SourcesPanel v={v} filter={{ chains, excluded: new Set(), onToggle: () => {} }} />,
    );
    const tagged = [...html.matchAll(/data-chain="([^"]+)"/g)].map((m) => m[1]!);
    expect(tagged).toHaveLength(7);
    expect(html.match(/type="checkbox"/g)).toHaveLength(7);

    // the tag must be a real chain root, or hovering cannot light the tree
    const roots = new Set(v.layout.chains.map((c) => c.root));
    expect(tagged.every((t) => roots.has(t))).toBe(true);
  });

  it("offers a start link for every chain and a mail link where one exists", () => {
    const { v, chains } = chainRows();
    const html = renderToStaticMarkup(
      <SourcesPanel v={v} filter={{ chains, excluded: new Set(), onToggle: () => {} }} />,
    );
    expect(html.match(/class="srclink" href="#/g)).toHaveLength(7);
    // the meeting notice never existed as an email, so it has no thread to link
    expect(html.match(/class="srclink" href="https/g)).toHaveLength(6);
  });

  it("reflects exclusions in the checked state", () => {
    const { v, chains } = chainRows();
    const excluded = new Set([chains[0]!.root]);
    const html = renderToStaticMarkup(
      <SourcesPanel v={v} filter={{ chains, excluded, onToggle: () => {} }} />,
    );
    expect(html.match(/checked=""/g)).toHaveLength(6);
  });

  it("omits the filter entirely when none is supplied, as in the static export", () => {
    const { v } = chainRows();
    const html = renderToStaticMarkup(<SourcesPanel v={v} />);
    expect(html).not.toContain("type=\"checkbox\"");
    expect(html).not.toContain("data-chain");
  });
});

describe("static export", () => {
  it("has no app-only controls", () => {
    const html = renderToStaticMarkup(<Timeline spec={spec} />);
    expect(html).not.toContain("spectog"); // json viewer button
    expect(html).not.toContain("type=\"checkbox\"");
  });

  it("still renders the whole transcript and the tree", () => {
    const html = renderToStaticMarkup(<Timeline spec={spec} />);
    expect(html.match(/class="hit"/g)).toHaveLength(58);
    expect(html.match(/class="chdr"/g)).toHaveLength(7);
    expect(html.match(/<a class="par"/g)).toHaveLength(51);
  });

  it("emits one avatar rule per face rather than inlining each image", () => {
    const html = renderToStaticMarkup(<Timeline spec={spec} />);
    expect(html.match(/\.av\.p\d\{background-image/g)).toHaveLength(5);
    expect(html).not.toContain('style="background-image');
  });
});

describe("participants panel", () => {
  const senders = () => new Set(spec.messages.map((m) => m.sender).filter(Boolean) as string[]);
  const panel = () => new Set((spec.participants ?? []).map((p) => p.name));

  it("lists every sender, and more besides", () => {
    // Both directions, because each catches a different failure. A sender the
    // panel omits is a name the page shows and then cannot account for; a panel
    // that is merely equal to the senders has stopped carrying the recipients,
    // which is the whole reason it exists.
    expect([...senders()].filter((s) => !panel().has(s))).toEqual([]);
    expect([...panel()].filter((p) => !senders().has(p)).length).toBeGreaterThan(0);
  });

  it("shows someone who only ever appears in To:/Cc:", () => {
    const html = renderToStaticMarkup(<Timeline spec={spec} />);
    const silent = [...panel()].filter((p) => !senders().has(p));
    for (const name of silent) expect(html).toContain(name);
  });

  it("no longer prints the per-org roster, which the panel supersedes", () => {
    const html = renderToStaticMarkup(<Timeline spec={spec} />);
    expect(html).not.toContain('class="roster"');
  });

  it("assigns the same avatar colour slots with the roster gone", () => {
    // The slot is the index of an org in first-appearance order over the
    // messages, which `derive` computes for itself; the roster only ever
    // displayed that order. These counts are the baseline from before it was
    // removed, so a change to where a colour comes from fails here.
    const html = renderToStaticMarkup(<Timeline spec={spec} />);
    const count = (slot: string) => (html.match(new RegExp(`class="av ${slot}`, "g")) ?? []).length;
    expect([count("o1"), count("o2"), count("o3"), count("o4"), count("o5")])
      .toEqual([36, 24, 5, 0, 5]);

    const v = derive(spec);
    const slotOf = new Map(v.rows.map((r) => [r.entry.org ?? "", r.orgSlot]));
    expect(Object.fromEntries(slotOf)).toEqual({
      Starfleet: "o1", Daystrom: "o2", "Utopia Planitia": "o3", "": "o5",
    });
  });
});
