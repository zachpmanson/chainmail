import { ArrowDownTrayIcon } from "@heroicons/react/24/outline";
import { graphLanes, type GraphNode } from "../../lib/timeline/lanes";
import type { Row, View } from "../../lib/timeline/derive";

/* step is the pitch along time, across the pitch between lanes. */
const X0 = 11;
const Y0 = 9;
const STEP = { v: 12, h: 14 } as const;
const ACROSS = { v: 11, h: 16 } as const;
const MIN_W = 96;
const orgFill = {
  o1: "fill-org-1",
  o2: "fill-org-2",
  o3: "fill-org-3",
  o4: "fill-org-4",
  o5: "fill-org-5",
} as const;
const orgStroke = {
  o1: "stroke-org-1",
  o2: "stroke-org-2",
  o3: "stroke-org-3",
  o4: "stroke-org-4",
  o5: "stroke-org-5",
} as const;
const colorClass = (classes: typeof orgFill | typeof orgStroke, slot: string) =>
  classes[slot as keyof typeof orgFill] ?? classes.o5;

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

const pos = (o: Orient, row: number, lane: number): [number, number] =>
  o === "v" ? [X0 + lane * ACROSS.v, Y0 + row * STEP.v] : [X0 + row * STEP.h, Y0 + lane * ACROSS.h];

const size = (o: Orient, rows: number, laneCount: number): [number, number] =>
  o === "v"
    ? [Math.max(X0 + (laneCount - 1) * ACROSS.v + 13, MIN_W), Y0 + (rows - 1) * STEP.v + 10]
    : [X0 + (rows - 1) * STEP.h + 13, Y0 + (laneCount - 1) * ACROSS.h + 10];

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

function capD(o: Orient, cx: number, cy: number): string {
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

export default function Minimap({ v }: { v: View }) {
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
      while (cur?.parent) {
        d++;
        cur = byId.get(cur.parent);
      }
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
        className="block"
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={`Reply tree of ${v.rows.length} entries, ${
          o === "v" ? "time downward" : "time rightward"
        }`}
      >
        {/* Hit strips first: .nd/.lk are pointer-events:none so the strip gets the pointer. */}
        {v.rows.map((r) => {
          const n = byId.get(r.id)!;
          const [cx, cy] = pos(o, rowOf.get(r.id)!, n.lane);
          const [w2, h2] = o === "v" ? [width, STEP.v] : [STEP.h, height];
          const [x2, y2] = o === "v" ? [0, cy - STEP.v / 2] : [cx - STEP.h / 2, 0];
          return (
            <rect
              key={`hit-${o}-${r.id}`}
              className="fill-transparent cursor-pointer hover:fill-quote"
              data-hit=""
              data-id={r.id}
              x={x2}
              y={y2}
              width={w2}
              height={h2}
            >
              <title>
                {`${r.entry.kind === "note" ? r.entry.label : r.entry.sender} — ` +
                  [r.entry.date, r.entry.time].filter(Boolean).join(" ") +
                  (n.isRoot ? " · starts a thread (no parent)" : "")}
              </title>
            </rect>
          );
        })}

        {v.rows.map((r) => {
          const n = byId.get(r.id)!;
          if (!n.parent) return null;
          const [x1, y1] = pos(o, rowOf.get(n.parent)!, byId.get(n.parent)!.lane);
          const [x2, y2] = pos(o, rowOf.get(r.id)!, n.lane);
          return (
            <path
              key={`lk-${o}-${r.id}`}
              className={`lk fill-none stroke-line stroke-[1.3] pointer-events-none${n.isFork ? " stroke-org-4 stroke-[1.6]" : ""}`}
              data-c={r.id}
              d={linkD(o, x1, y1, x2, y2)}
            />
          );
        })}

        {v.rows.map((r) => {
          const n = byId.get(r.id)!;
          const note = r.entry.kind === "note";
          const [cx, cy] = pos(o, rowOf.get(r.id)!, n.lane);
          return (
            <g
              key={`nd-${o}-${r.id}`}
              className={["nd", r.orgSlot].filter(Boolean).join(" ")}
              data-id={r.id}
              data-p={n.parent ?? ""}
            >
              {n.isRoot ? (
                <path
                  className="pointer-events-none fill-none stroke-muted stroke-[1.6] opacity-[.85]"
                  d={capD(o, cx, cy)}
                />
              ) : null}
              {note ? (
                <rect
                  className="pointer-events-none fill-muted stroke-card stroke-[1.4]"
                  x={cx - 3.5}
                  y={cy - 3.5}
                  width={7}
                  height={7}
                  transform={`rotate(45 ${cx} ${cy})`}
                />
              ) : (
                <circle
                  className={`pointer-events-none cursor-pointer stroke-card stroke-[1.4] ${r.entry.quoted ? `fill-card stroke-[1.9] ${colorClass(orgStroke, r.orgSlot)}` : colorClass(orgFill, r.orgSlot)}`}
                  cx={cx}
                  cy={cy}
                  r={3.9}
                />
              )}
            </g>
          );
        })}
      </svg>
    );
  };

  const tallyLegend = (
    <div className="foot2 flex flex-col gap-1 border-t border-line px-2 pt-1 pb-1.5 text-[.58rem] leading-[1.25] text-muted">
      <div className="flex flex-col gap-0.5 whitespace-nowrap">
        <div>
          <b className="font-semibold text-fg">{g.roots}</b> chains
        </div>
        <div>
          <b className="font-semibold text-fg">{g.laneCount}</b> lanes
        </div>
        <div>
          <b className="font-semibold text-fg">{deepest}</b> deep
        </div>
        <div>
          <b className="font-semibold text-fg">{g.forks}</b> forks
        </div>
        <div>
          <b className="font-semibold text-fg">{g.leaves}</b> dead ends
        </div>
      </div>
      <dl className="m-0 flex flex-col gap-0.5">
        <div className="flex items-center gap-1.5">
          <svg className="h-[.7em] w-[.7em] shrink-0" viewBox="0 0 10 10" aria-hidden="true">
            <circle cx="5" cy="5" r="2.9" fill="currentColor" />
          </svg>
          <dt className="m-0 text-muted">message</dt>
        </div>
        <div className="flex items-center gap-1.5">
          <svg className="h-[.7em] w-[.7em] shrink-0" viewBox="0 0 10 10" aria-hidden="true">
            <rect
              x="2.6"
              y="2.6"
              width="4.8"
              height="4.8"
              fill="currentColor"
              transform="rotate(45 5 5)"
            />
          </svg>
          <dt className="m-0 text-muted">note</dt>
        </div>
        <div className="flex items-center gap-1.5">
          <svg className="h-[.7em] w-[.7em] shrink-0" viewBox="0 0 10 10" aria-hidden="true">
            <path d="M2 2.6 H8" stroke="currentColor" strokeWidth="1.1" />
            <circle cx="5" cy="5.5" r="2.5" fill="currentColor" />
          </svg>
          <dt className="m-0 text-muted">starts thread</dt>
        </div>
        <div className="flex items-center gap-1.5">
          <svg className="h-[.7em] w-[.7em] shrink-0" viewBox="0 0 10 10" aria-hidden="true">
            <circle cx="5" cy="5" r="2.9" fill="none" stroke="currentColor" strokeWidth="1.2" />
          </svg>
          <dt className="m-0 text-muted">reconstructed</dt>
        </div>
      </dl>
    </div>
  );

  return (
    <aside
      className="mini fixed top-0 right-0 bottom-0 z-[30] flex w-max max-w-[16.5rem] flex-col border-l border-line bg-card print:hidden max-[1024px]:top-[2.4rem] max-[1024px]:bg-card/86"
      id="mini"
    >
      <h3 className="m-0 flex items-center gap-1.5 border-b border-line px-3 pt-2 pb-1.5 text-[.66rem] font-bold uppercase tracking-[.09em] text-muted">
        Reply tree<span className="ml-auto font-semibold opacity-75">{v.rows.length}</span>
        <button
          type="button"
          className="inline-flex cursor-pointer items-center rounded border border-transparent bg-transparent px-1 py-px font-[inherit] text-muted hover:border-line hover:bg-card hover:text-accent"
          title="Download this reply tree as an SVG file"
          aria-label="Download this reply tree as an SVG file"
          onClick={download}
        >
          <ArrowDownTrayIcon width={11} height={11} aria-hidden="true" />
        </button>
      </h3>
      {/* Both orientations render; body.tree-h picks one in CSS, so no React state. */}
      <div className="vrow flex min-h-0 flex-1 flex-col">
        <div className="mbody min-h-0 flex-1 overflow-auto px-1 pt-2 pb-3">{treeSvg("v")}</div>
        {tallyLegend}
      </div>
      <div className="hrow hidden min-h-0 flex-1">
        <div className="mbody min-h-0 flex-1 overflow-auto px-1 pt-2 pb-3">{treeSvg("h")}</div>
        {tallyLegend}
      </div>
    </aside>
  );
}
