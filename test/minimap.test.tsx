import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { renderToStaticMarkup } from "react-dom/server";
import { Minimap, treeSvgString } from "../src/components/Minimap";
import { derive } from "../src/lib/derive";
import { graphLanes } from "../src/lib/lanes";
import { normalise } from "../src/lib/normalise";

const spec = normalise(JSON.parse(readFileSync("fixtures/synthetic.json", "utf8")));

function view() {
  return derive(spec);
}

function graph() {
  const v = view();
  // the component derives the graph itself; mirror the call so the export input
  // matches what the panel would hand it
  const g = graphLanes(
    v.rows.map((r) => r.entry),
    (e) => v.rows.find((r) => r.entry === e)!.id,
  );
  return { v, g };
}

describe("minimap export button", () => {
  it("sits in the panel header and names its action", () => {
    const html = renderToStaticMarkup(<Minimap v={view()} />);
    const btn = html.match(/<button[^>]*class="xsvg"[^>]*>/)?.[0];
    expect(btn).toBeTruthy();
    expect(btn).toContain('aria-label="Download this reply tree as an SVG file"');
    // it lives inside the tree panel, not anywhere else on the page
    expect(html.indexOf('id="mini"')).toBeLessThan(html.indexOf('class="xsvg"'));
  });
});

describe("treeSvgString", () => {
  it("draws one node per row plus the root caps, links for replies", () => {
    const { v, g } = graph();
    const deepest = 1;
    const svg = treeSvgString({ title: v.title, rows: v.rows, nodes: g.nodes, laneCount: g.laneCount, deepest, dark: false });

    const circles = svg.match(/<circle /g) ?? [];
    const rects = svg.match(/<rect /g) ?? [];
    const nodes = svg.match(/<circle /g)!.length;
    const roots = g.nodes.filter((n) => n.isRoot).length;
    const notes = v.rows.filter((r) => r.entry.kind === "note").length;
    const msgs = v.rows.length - notes;

    // every message is a circle and every note a rotated rect; the footer's
    // legend adds three circles and one rect, and the canvas has a background rect
    expect(circles.length).toBe(msgs + 3);
    expect(rects.length).toBe(notes + 2);
    expect(roots).toBeGreaterThan(0);
    void nodes;
  });

  it("renders the tally and legend, and the tally matches the graph", () => {
    const { v, g } = graph();
    const deepest = 1;
    const svg = treeSvgString({ title: v.title, rows: v.rows, nodes: g.nodes, laneCount: g.laneCount, deepest, dark: false });

    for (const label of ["chains", "lanes", "deep", "forks", "dead ends"]) {
      expect(svg).toContain(label);
    }
    for (const label of ["message", "note", "starts chain", "reconstructed"]) {
      expect(svg).toContain(label);
    }
    // the count in the title row is the number of entries
    expect(svg).toContain(`>${v.rows.length}<`);
  });

  it("is well-formed XML with a size and a card background", () => {
    const { v, g } = graph();
    const svg = treeSvgString({ title: v.title, rows: v.rows, nodes: g.nodes, laneCount: g.laneCount, deepest: 1, dark: false });
    expect(svg.startsWith("<svg xmlns=")).toBe(true);
    expect(svg.trimEnd().endsWith("</svg>")).toBe(true);
    expect(svg).toMatch(/<rect width="[\d.]+" height="[\d.]+" fill="#fff"\/>/);
  });

  it("switches palette with the dark flag", () => {
    const { v, g } = graph();
    const light = treeSvgString({ title: v.title, rows: v.rows, nodes: g.nodes, laneCount: g.laneCount, deepest: 1, dark: false });
    const dark = treeSvgString({ title: v.title, rows: v.rows, nodes: g.nodes, laneCount: g.laneCount, deepest: 1, dark: true });
    expect(light).toMatch(/fill="#fff"/);
    expect(dark).toMatch(/fill="#1d1c21"/);
  });
});
