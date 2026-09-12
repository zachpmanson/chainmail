import { graphLanes, type GraphNode } from "../lib/lanes";
import type { Row, View } from "../lib/derive";

/* The tree panel's two live geometries. Time runs down the page in vertical
   mode (the right-edge panel) and rightward in horizontal mode (the bottom
   strip); step is the pitch along time, across the pitch between lanes. The
   horizontal lane pitch is larger because a bottom strip has the depth to
   spare and crossing-branch links want the room. */
const X0 = 11;
const Y0 = 9;
const STEP = { v: 12, h: 14 } as const;
const ACROSS = { v: 11, h: 16 } as const;
/* keep just enough floor for a single chain lane to stay readable; the svg is
   otherwise sized to its lane count so the overlay panel can hug the tree */
const MIN_W = 96;

export type Orient = "v" | "h";

/**
 * The tree panel's palette as concrete hex, for the standalone export. The
 * on-screen svg is styled by CSS classes over :root variables; a downloaded
 * .svg has neither, so every fill and stroke has to be baked in at export
 * time. Values mirror styles.css's light and dark themes, and the background
 * is the panel's own card colour rather than the page's.
 */
const PALETTES = {
  light: {
    bg: "#fff", fg: "#1e1c1a", muted: "#6f6963", line: "#e3ded7",
    o1: "#2f6f5f", o2: "#4a5b9c", o3: "#8a5a2b", o4: "#8c4a6b", o5: "#77716a",
  },
  dark: {
    bg: "#1d1c21", fg: "#ece9e4", muted: "#9d968e", line: "#2d2a31",
    o1: "#3d8d78", o2: "#5f74c4", o3: "#a9713a", o4: "#b8688c", o5: "#4d4a52",
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

/* Shared geometry: node position, panel size, link path and root cap for an
   orientation. The two orientations are transposes of each other — row (time)
   and lane (across) swap axes — so one pair of functions covers both. */

const pos = (o: Orient, row: number, lane: number): [number, number] =>
  o === "v"
    ? [X0 + lane * ACROSS.v, Y0 + row * STEP.v]
    : [X0 + row * STEP.h, Y0 + lane * ACROSS.h];

const size = (o: Orient, rows: number, laneCount: number): [number, number] =>
  o === "v"
    ? [Math.max(X0 + (laneCount - 1) * ACROSS.v + 13, MIN_W), Y0 + (rows - 1) * STEP.v + 10]
    : [X0 + (rows - 1) * STEP.h + 13, Y0 + (laneCount - 1) * ACROSS.h + 10];

/** The connector from a parent node to one of its children. Vertical mode
 *  descends then jogs across; horizontal mode runs right then jogs down. */
function linkD(o: Orient, xp: number, yp: number, xc: number, yc: number): string {
  if (o === "v") {
    return Math.abs(xp - xc) < 0.5
      ? `M${xp} ${yp + 4.4} V${yc - 4.4}`
      : `M${xp} ${yp + 4.4} V${yc - 4.5} Q${xp} ${yc} ${xp + 4.5} ${yc} H${xc - 4}`;
  }
  return Math.abs(yp - yc) < 0.5
    ? `M${xp + 4.4} ${yp} H${xc - 4.4}`
    : `M${xp + 4.4} ${yp} H${xc - 4.5} Q${xc} ${yp} ${xc} ${yp + 4.5} V${yc - 4}`;
}

/** The cap marking a chain start: a bar above the node vertically, beside it
 *  (upstream, on the time side) horizontally. */
function capD(o: Orient, cx: number, cy: number): string {
  return o === "v"
    ? `M${cx - 4.6} ${cy - 6.6} H${cx + 4.6}`
    : `M${cx - 6.6} ${cy - 4.6} V${cy + 4.6}`;
}

/**
 * A standalone SVG of the whole reply-tree panel — the graph, the tally
 * (chains / lanes / deep / forks / dead ends) and the legend (message, note,
 * starts chain, reconstructed) — with every style baked in, since a downloaded
 * .svg carries no page CSS. Horizontal mode lays the tree out left-to-right,
 * matching the bottom strip.
 */
export function treeSvgString(o: TreeExport): string {
  const pal: Palette = PALETTES[o.dark ? "dark" : "light"];
  const orient: Orient = o.horizontal ? "h" : "v";
  const byId = new Map(o.nodes.map((n) => [n.id, n]));
  const rowOf = new Map(o.rows.map((r, i) => [r.id, i]));
  const at = (id: string): [number, number] =>
    pos(orient, rowOf.get(id)!, byId.get(id)!.lane);

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
  const legend = ["message", "note", "starts chain", "reconstructed"];

  const M = 14; // outer margin
  // Title and divider sit on their own offsets (RULE_Y = label + 10); the tree
  // starts on its own TREE_TOP so the gaps above and below the rule can be
  // tuned independently of each other. Similarly the footer divider floats
  // close under the tree while the footer text keeps its own offset.
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
    line(`${icon}<text x="${lx + 14}" y="${yy}" font-size="11" fill="${pal.muted}">${label}</text>`);
  });

  line("</svg>");
  return parts.join("\n");
}

/** The current colour scheme, honouring an explicit data-theme first. */
export function prefersDark(doc: Document = document): boolean {
  const theme = doc.documentElement.getAttribute("data-theme");
  if (theme) return theme === "dark";
  return typeof matchMedia === "function" && matchMedia("(prefers-color-scheme: dark)").matches;
}

/** The page title, made safe for a file name: "reply-tree-<slug>.svg". */
function fileName(title: string): string {
  const slug =
    title
      .replace(/^#+/, "")
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "tree";
  return `reply-tree-${slug}.svg`;
}

/**
 * A sticky index of the reply graph, in either orientation. Vertical: down is
 * time, so rows follow the transcript's own order and the panel is
 * row-aligned with the page. Horizontal: right is time; the panel is a bottom
 * strip and scrolls lengthwise. Across (lanes) is only lane allocation in
 * both: concurrently-live chains, and somewhere for a fork to go.
 */
export function Minimap({ v }: { v: View }) {
  const g = graphLanes(
    v.rows.map((r) => r.entry),
    (e) => v.rows.find((r) => r.entry === e)!.id,
  );
  const byId = new Map(g.nodes.map((n) => [n.id, n]));
  const rowOf = new Map(v.rows.map((r, i) => [r.id, i]));
  const deepest = Math.max(
    ...v.rows.map((r) => {
      let d = 0;
      let cur = byId.get(r.id);
      while (cur?.parent) { d++; cur = byId.get(cur.parent); }
      return d + 1;
    }),
  );

  const download = () => {
    const svg = treeSvgString({
      title: v.title,
      rows: v.rows,
      nodes: g.nodes,
      laneCount: g.laneCount,
      deepest,
      dark: prefersDark(),
      // the export follows the live panel: the horizontal strip exports the
      // rotated geometry, the right-edge panel the vertical one
      horizontal: document.body.classList.contains("tree-h"),
    });
    const blob = new Blob([svg], { type: "image/svg+xml;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = fileName(v.title);
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  };

  /** The reply graph as an svg element for one orientation. */
  const treeSvg = (o: Orient) => {
    const [width, height] = size(o, v.rows.length, g.laneCount);
    return (
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={`Reply tree of ${v.rows.length} entries, ${
          o === "v" ? "time downward" : "time rightward"
        }`}
      >
        {/* hit strips first: the hover band paints behind the dots, and .nd/.lk
            are pointer-events:none so the strip always receives the pointer.
            A strip spans the whole panel across time's axis — the full row
            vertically, the full column horizontally. */}
        {v.rows.map((r) => {
          const n = byId.get(r.id)!;
          const [cx, cy] = pos(o, rowOf.get(r.id)!, n.lane);
          const [w2, h2] = o === "v"
            ? [width, STEP.v]
            : [STEP.h, height];
          const [x2, y2] = o === "v"
            ? [0, cy - STEP.v / 2]
            : [cx - STEP.h / 2, 0];
          return (
            <rect
              key={`hit-${o}-${r.id}`}
              className="hit"
              data-id={r.id}
              x={x2}
              y={y2}
              width={w2}
              height={h2}
            >
              <title>
                {`${r.entry.kind === "note" ? r.entry.label : r.entry.sender} — ` +
                  [r.entry.date, r.entry.time].filter(Boolean).join(" ") +
                  (n.isRoot ? " · starts a chain (no parent)" : "")}
              </title>
            </rect>
          );
        })}

        {v.rows.map((r) => {
          const n = byId.get(r.id)!;
          if (!n.parent) return null;
          const [x1, y1] = pos(o, rowOf.get(n.parent)!, byId.get(n.parent)!.lane);
          const [x2, y2] = pos(o, rowOf.get(r.id)!, n.lane);
          const cls = `lk${n.isFork ? " fk" : ""}`;
          return <path key={`lk-${o}-${r.id}`} className={cls} data-c={r.id} d={linkD(o, x1, y1, x2, y2)} />;
        })}

        {v.rows.map((r) => {
          const n = byId.get(r.id)!;
          const note = r.entry.kind === "note";
          const cls = [
            "nd",
            r.orgSlot,
            note && "sysn",
            r.entry.quoted && "qd",
            n.isRoot && "rt",
          ]
            .filter(Boolean)
            .join(" ");
          const [cx, cy] = pos(o, rowOf.get(r.id)!, n.lane);
          return (
            <g key={`nd-${o}-${r.id}`} className={cls} data-id={r.id} data-p={n.parent ?? ""}>
              {n.isRoot ? <path className="rtcap" d={capD(o, cx, cy)} /> : null}
              {note ? (
                <rect
                  x={cx - 3.5}
                  y={cy - 3.5}
                  width={7}
                  height={7}
                  transform={`rotate(45 ${cx} ${cy})`}
                />
              ) : (
                <circle cx={cx} cy={cy} r={3.9} />
              )}
            </g>
          );
        })}
      </svg>
    );
  };

  const tallyLegend = (
    <div className="foot2">
      <div className="tally">
        <div><b>{g.roots}</b> chains</div>
        <div><b>{g.laneCount}</b> lanes</div>
        <div><b>{deepest}</b> deep</div>
        <div><b>{g.forks}</b> forks</div>
        <div><b>{g.leaves}</b> dead ends</div>
      </div>
      <dl className="legend">
        <div><svg className="lg" viewBox="0 0 10 10" aria-hidden="true"><circle cx="5" cy="5" r="2.9" fill="currentColor"/></svg><dt>message</dt></div>
        <div><svg className="lg" viewBox="0 0 10 10" aria-hidden="true"><rect x="2.6" y="2.6" width="4.8" height="4.8" fill="currentColor" transform="rotate(45 5 5)"/></svg><dt>note</dt></div>
        <div><svg className="lg" viewBox="0 0 10 10" aria-hidden="true"><path d="M2 2.6 H8" stroke="currentColor" strokeWidth="1.1"/><circle cx="5" cy="5.5" r="2.5" fill="currentColor"/></svg><dt>starts chain</dt></div>
        <div><svg className="lg" viewBox="0 0 10 10" aria-hidden="true"><circle cx="5" cy="5" r="2.9" fill="none" stroke="currentColor" strokeWidth="1.2"/></svg><dt>reconstructed</dt></div>
      </dl>
    </div>
  );

  return (
    <aside className="mini" id="mini">
      <h3>
        Reply tree<span className="ct">{v.rows.length}</span>
        <button
          type="button"
          className="xsvg"
          title="Download this reply tree as an SVG file"
          aria-label="Download this reply tree as an SVG file"
          onClick={download}
        >
          <svg viewBox="0 0 16 16" width="11" height="11" aria-hidden="true">
            <path d="M8 2.5 V10 M4.5 7 8 10.5 11.5 7" fill="none"
              stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
            <path d="M3 12.5 H13" fill="none" stroke="currentColor"
              strokeWidth="1.4" strokeLinecap="round" />
          </svg>
        </button>
      </h3>
      {/* both orientations are in the DOM; CSS shows the live one (body.tree-h
          swaps to the horizontal row) so behaviour.js needs no React state */}
      <div className="vrow">
        <div className="mbody">{treeSvg("v")}</div>
        {tallyLegend}
      </div>
      <div className="hrow">
        <div className="mbody">{treeSvg("h")}</div>
        {tallyLegend}
      </div>
    </aside>
  );
}