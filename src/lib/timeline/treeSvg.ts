import type { Row } from "./derive";
import type { GraphNode } from "./lanes";

/* step is the pitch along time, across the pitch between lanes. */
const X0 = 11;
const Y0 = 9;
export const STEP = { v: 12, h: 14 } as const;
const ACROSS = { v: 11, h: 16 } as const;
const MIN_W = 96;

export type Orient = "v" | "h";

/** A downloaded .svg has no page CSS, so colours are baked in; keep in step with styles.css. */
const PALETTES = {
  light: {
    bg: "#fff",
    fg: "#1e1c1a",
    muted: "#6f6963",
    line: "#e3ded7",
    o1: "#2f6f5f",
    o2: "#4a5b9c",
    o3: "#8a5a2b",
    o4: "#8c4a6b",
    o5: "#77716a",
  },
  dark: {
    bg: "#1d1c21",
    fg: "#ece9e4",
    muted: "#9d968e",
    line: "#2d2a31",
    o1: "#3d8d78",
    o2: "#5f74c4",
    o3: "#a9713a",
    o4: "#b8688c",
    o5: "#4d4a52",
  },
} as const;
type Palette = (typeof PALETTES)[keyof typeof PALETTES];

/** Rough advance of one character at the footer's font size, for sizing columns. */
const CHAR_W = 6.1;

const esc = (s: string) =>
  s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]!);

export interface TreeExport {
  /** page title, drawn above the tree and used in the download's file name */
  title: string;
  /** the rows, in transcript order, carrying the kind/colour the tree draws */
  rows: Row[];
  /** per-entry nodes, as graphLanes laid them out */
  nodes: GraphNode[];
  laneCount: number;
  /** maximum reply depth (inclusive), the panel's "deep" stat */
  deepest: number;
  dark: boolean;
  /** horizontal mode lays time rightward instead of downward */
  horizontal?: boolean;
}

/* The two orientations are transposes (row and lane swap axes). */

export const pos = (o: Orient, row: number, lane: number): [number, number] =>
  o === "v" ? [X0 + lane * ACROSS.v, Y0 + row * STEP.v] : [X0 + row * STEP.h, Y0 + lane * ACROSS.h];

export const size = (o: Orient, rows: number, laneCount: number): [number, number] =>
  o === "v"
    ? [Math.max(X0 + (laneCount - 1) * ACROSS.v + 13, MIN_W), Y0 + (rows - 1) * STEP.v + 10]
    : [X0 + (rows - 1) * STEP.h + 13, Y0 + (laneCount - 1) * ACROSS.h + 10];

export function linkD(o: Orient, xp: number, yp: number, xc: number, yc: number): string {
  if (o === "v") {
    return Math.abs(xp - xc) < 0.5
      ? `M${xp} ${yp + 4.4} V${yc - 4.4}`
      : `M${xp} ${yp + 4.4} V${yc - 4.5} Q${xp} ${yc} ${xp + 4.5} ${yc} H${xc - 4}`;
  }
  return Math.abs(yp - yc) < 0.5
    ? `M${xp + 4.4} ${yp} H${xc - 4.4}`
    : `M${xp + 4.4} ${yp} H${xc - 4.5} Q${xc} ${yp} ${xc} ${yp + 4.5} V${yc - 4}`;
}

export function capD(o: Orient, cx: number, cy: number): string {
  return o === "v"
    ? `M${cx - 4.6} ${cy - 6.6} H${cx + 4.6}`
    : `M${cx - 6.6} ${cy - 4.6} V${cy + 4.6}`;
}

export function treeSvgString(o: TreeExport): string {
  const pal: Palette = PALETTES[o.dark ? "dark" : "light"];
  const orient: Orient = o.horizontal ? "h" : "v";
  const byId = new Map(o.nodes.map((n) => [n.id, n]));
  const rowOf = new Map(o.rows.map((r, i) => [r.id, i]));
  const at = (id: string): [number, number] => pos(orient, rowOf.get(id)!, byId.get(id)!.lane);

  const [treeW, treeH] = size(orient, o.rows.length, o.laneCount);

  const roots = o.nodes.filter((n) => n.isRoot).length;
  const forks = o.nodes.filter((n) => n.isFork).length;
  const leaves = o.nodes.filter((n) => n.isLeaf).length;
  const tally: [string, string][] = [
    [`${roots}`, "chains"],
    [`${o.laneCount}`, "lanes"],
    [`${o.deepest}`, "deep"],
    [`${forks}`, "forks"],
    [`${leaves}`, "dead ends"],
  ];
  const legend = ["message", "note", "starts thread", "reconstructed"];

  const M = 14; // outer margin
  const RULE_Y = 34; // the title rule, between the label and the tree
  const TREE_TOP = 48; // first row's centre: title + rule + room under it
  const ROW_H = 17; // footer row pitch
  const tallyCol = Math.max(...tally.map(([n, l]) => `${n} ${l}`.length)) * CHAR_W;
  const legendCol = Math.max(...legend.map((s) => s.length)) * CHAR_W + 22;
  const W = Math.round(Math.max(treeW, tallyCol + 24 + legendCol) + M * 2);
  const treeBottom = TREE_TOP + treeH - Y0;
  const footerY = treeBottom + 24; // divider + breathing room; first row baseline
  const H = Math.round(footerY + 4 * ROW_H + 6);

  const color = (slot: string) => (pal as Record<string, string>)[slot] ?? pal.o5;

  const parts: string[] = [];
  const line = (s: string) => parts.push(s);

  line(
    `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" ` +
      `font-family="-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif">`,
  );
  line(`<title>${esc(o.title)} reply tree</title>`);
  line(`<rect width="${W}" height="${H}" fill="${pal.bg}"/>`);

  // title row: name left, entry count right, matching the panel's header
  line(
    `<text x="${M}" y="${M + 10}" font-size="12" font-weight="700" fill="${pal.muted}" ` +
      `letter-spacing="1.1" text-transform="uppercase">Reply tree</text>`,
  );
  line(
    `<text x="${W - M}" y="${M + 10}" text-anchor="end" font-size="12" font-weight="600" ` +
      `fill="${pal.muted}" opacity=".75">${o.rows.length}</text>`,
  );
  line(
    `<line x1="${M}" y1="${RULE_Y}" x2="${W - M}" y2="${RULE_Y}" stroke="${pal.line}" stroke-width="1"/>`,
  );

  // the graph, exactly as the panel draws it
  line(`<g transform="translate(${M - X0} ${TREE_TOP - Y0})">`);
  for (const r of o.rows) {
    const n = byId.get(r.id)!;
    if (!n.parent) continue;
    const [x1, y1] = at(n.parent);
    const [x2, y2] = at(r.id);
    const attrs = n.isFork
      ? `stroke="${pal.o4}" stroke-width="1.6"`
      : `stroke="${pal.line}" stroke-width="1.3"`;
    line(`<path d="${linkD(orient, x1, y1, x2, y2)}" fill="none" ${attrs}/>`);
  }
  for (const r of o.rows) {
    const n = byId.get(r.id)!;
    const note = r.entry.kind === "note";
    const [cx, cy] = at(r.id);
    if (n.isRoot) {
      line(
        `<path d="${capD(orient, cx, cy)}" stroke="${pal.muted}" stroke-width="1.6" opacity=".85"/>`,
      );
    }
    if (note) {
      line(
        `<rect x="${cx - 3.5}" y="${cy - 3.5}" width="7" height="7" transform="rotate(45 ${cx} ${cy})" ` +
          `fill="${pal.muted}" stroke="${pal.bg}" stroke-width="1.4"/>`,
      );
    } else {
      const c = color(r.orgSlot);
      line(
        r.entry.quoted
          ? `<circle cx="${cx}" cy="${cy}" r="3.9" fill="${pal.bg}" stroke="${c}" stroke-width="1.9"/>`
          : `<circle cx="${cx}" cy="${cy}" r="3.9" fill="${c}" stroke="${pal.bg}" stroke-width="1.4"/>`,
      );
    }
  }
  line("</g>");

  // footer: divider, then the tally and legend side by side, as on the panel
  line(
    `<line x1="${M}" y1="${treeBottom + 6}" x2="${W - M}" y2="${treeBottom + 6}" stroke="${pal.line}" stroke-width="1"/>`,
  );
  tally.forEach(([n, label], i) => {
    const yy = footerY + i * ROW_H;
    line(
      `<text x="${M}" y="${yy}" font-size="11" fill="${pal.muted}">` +
        `<tspan font-weight="600" fill="${pal.fg}">${n}</tspan> ${label}</text>`,
    );
  });
  const lx = M + tallyCol + 24;
  legend.forEach((label, i) => {
    const yy = footerY + i * ROW_H;
    const cy = yy - 4;
    const icon = [
      `<circle cx="${lx + 5}" cy="${cy}" r="3.4" fill="${pal.muted}"/>`,
      `<rect x="${lx + 1.6}" y="${cy - 3.4}" width="6.8" height="6.8" transform="rotate(45 ${lx + 5} ${cy})" fill="${pal.muted}"/>`,
      `<path d="M${lx + 2} ${cy - 3.8} H${lx + 8}" stroke="${pal.muted}" stroke-width="1.1"/><circle cx="${lx + 5}" cy="${cy}" r="2.6" fill="${pal.muted}"/>`,
      `<circle cx="${lx + 5}" cy="${cy}" r="3.2" fill="none" stroke="${pal.muted}" stroke-width="1.3"/>`,
    ][i]!;
    line(
      `${icon}<text x="${lx + 14}" y="${yy}" font-size="11" fill="${pal.muted}">${label}</text>`,
    );
  });

  line("</svg>");
  return parts.join("\n");
}

/** The page title, made safe for a file name: "reply-tree-<slug>.svg". */
export function treeFileName(title: string): string {
  const slug =
    title
      .replace(/^#+/, "")
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "tree";
  return `reply-tree-${slug}.svg`;
}
