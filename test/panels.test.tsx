import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { renderToStaticMarkup } from "react-dom/server";
import { ParticipantsPanel, SourcesPanel } from "../src/components/Panels";
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
    // displayed that order. This map is the regression that matters: colouring
    // the panel must not move an org the transcript already placed.
    const v = derive(spec);
    const slotOf = new Map(v.rows.map((r) => [r.entry.org ?? "", r.orgSlot]));
    expect(Object.fromEntries(slotOf)).toEqual({
      Starfleet: "o1", Daystrom: "o2", "Utopia Planitia": "o3", "": "o5",
    });

    const html = renderToStaticMarkup(<Timeline spec={spec} />);
    const count = (slot: string) => (html.match(new RegExp(`class="av ${slot}`, "g")) ?? []).length;
    expect([count("o1"), count("o2"), count("o3"), count("o4"), count("o5")])
      .toEqual([36, 28, 6, 0, 0]);
    // Every o5 avatar the page used to carry was a panel row for someone who
    // sent nothing, coloured as though their org were unknown when the heading
    // above them named it. There is no org-less face left on this fixture, so
    // the unknown slot going empty is the whole of that defect leaving.
    expect(count("o5")).toBe(0);
  });

  /** slot -> name, for every avatar the participants panel renders. */
  function panelFaces() {
    const html = renderToStaticMarkup(<ParticipantsPanel v={derive(spec)} />);
    const re = /class="av (o\d)[^"]*">(?:<span class="ini">[^<]*<\/span>)?<\/div><span title="[^"]*">([^<]+)<\/span>/g;
    return new Map([...html.matchAll(re)].map((m) => [m[2]!, m[1]!]));
  }

  it("colours a person's panel row with the slot their own bubbles use", () => {
    // This is the point of the whole feature: the panel is read as the key to
    // the transcript, so a row in one colour and that person's messages in
    // another makes the key lie. Both sides come from one org -> slot map, and
    // the assertion is that they land on the same value in the rendered page.
    const v = derive(spec);
    const faces = panelFaces();
    const bubbles = new Map<string, Set<string>>();
    for (const r of v.rows) {
      if (!r.entry.sender) continue;
      (bubbles.get(r.entry.sender) ?? bubbles.set(r.entry.sender, new Set()).get(r.entry.sender)!)
        .add(r.orgSlot);
    }

    const checked: string[] = [];
    for (const [name, slots] of bubbles) {
      expect(faces.get(name), `${name} is a sender with no panel row`).toBeDefined();
      // One slot per sender, or "the slot their bubbles use" would not be a
      // single thing to agree with.
      expect([...slots], `${name} has bubbles in more than one colour`).toHaveLength(1);
      expect(faces.get(name), `${name}'s panel row disagrees with their bubbles`)
        .toBe([...slots][0]);
      checked.push(name);
    }
    expect(checked.length).toBeGreaterThan(5);
  });

  it("colours a recipient who sent nothing by their org, not as org-unknown", () => {
    // Their colleagues are coloured and they are listed under the same heading,
    // so leaving them on the unknown grey said their org was unknown when the
    // line directly above them named it. They get no bubble to agree with, which
    // is exactly why the colour has to come from their own participants row.
    const senders = new Set(spec.messages.map((m) => m.sender).filter(Boolean) as string[]);
    const faces = panelFaces();
    const silent = (spec.participants ?? []).filter((p) => !senders.has(p.name));
    expect(silent.length).toBeGreaterThan(0);
    for (const p of silent) {
      expect(faces.get(p.name), `${p.name} is missing from the panel`).toBeDefined();
      expect(faces.get(p.name), `${p.name} at ${p.org} is coloured as org-unknown`)
        .toBe(derive(spec).orgSlot(p.org));
      expect(faces.get(p.name)).not.toBe("o5");
    }
  });

  it("gives a panel-only org its own slot, after the orgs the messages placed", () => {
    // A cc-only recipient can be at an org no message came from. Appending keeps
    // the transcript's own order intact — the alternative, one order over
    // messages and participants together, would let adding a recipient repaint
    // bubbles that have nothing to do with them.
    const base = derive(spec);
    const withGuest = derive({
      ...spec,
      participants: [...(spec.participants ?? []), { name: "Guinan", org: "Ten Forward" }],
    });
    expect(withGuest.orgs).toEqual([...base.orgs, "Ten Forward"]);
    for (const org of base.orgs) expect(withGuest.orgSlot(org)).toBe(base.orgSlot(org));
    expect(withGuest.orgSlot("Ten Forward")).toBe("o4");
    // An org off the end of the list must not name a slot with no colour behind
    // it, which is what indexOf === -1 would arithmetic its way into.
    expect(base.orgSlot("Ten Forward")).toBe("o5");
  });

  it("puts a participant with no org on the unknown slot, which a fifth org shares", () => {
    // Known and deliberately not widened here: the cap at five predates the
    // panel, and o5 means both "no org" and "the fifth org onwards". A trail
    // with five or more orgs cannot tell the two apart by colour. The panel
    // still distinguishes them in words — the group heading names the org, or
    // says "Other" — so nothing is unrecoverable, only uncoloured.
    const v = derive({
      ...spec,
      participants: [{ name: "Nobody Known" }, ...(spec.participants ?? [])],
    });
    expect(v.orgSlot(undefined)).toBe("o5");

    const many = derive({
      ...spec,
      participants: [
        ...(spec.participants ?? []),
        { name: "D", org: "Four" }, { name: "E", org: "Five" }, { name: "F", org: "Six" },
      ],
    });
    expect(many.orgSlot("Five")).toBe("o5");
    expect(many.orgSlot("Six")).toBe("o5");
    expect(many.orgSlot(undefined)).toBe("o5");
  });

  it("tints each group heading with that group's slot, and leaves Other muted", () => {
    // One heading per org rather than a mark per row. The transcript is 57
    // bubbles, where a strip per bubble reads as structure; this is a dozen
    // rows, where a strip per row would read as noise, and the avatars already
    // carry the colour per person.
    const html = renderToStaticMarkup(<ParticipantsPanel v={derive(spec)} />);
    const heads = [...html.matchAll(/class="ogh ([^"]*)">([^<]+)</g)].map((m) => [m[2], m[1]]);
    expect(heads).toEqual([
      ["Starfleet", "o1"], ["Daystrom", "o2"], ["Utopia Planitia", "o3"],
    ]);

    // "Other" resolves to o5, and .ogh has no o5 rule: --o5 is an avatar fill
    // behind white text and is unreadable as .66rem text on the dark theme's
    // background, where --muted is not.
    const other = renderToStaticMarkup(
      <ParticipantsPanel v={derive({ ...spec, participants: [{ name: "Nobody Known" }] })} />,
    );
    expect(other).toContain('class="ogh o5">Other<');
    expect(readFileSync("src/styles.css", "utf8")).not.toMatch(/^\s*\.ogh\.o5\s*\{/m);
  });
});
