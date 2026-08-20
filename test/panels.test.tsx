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
