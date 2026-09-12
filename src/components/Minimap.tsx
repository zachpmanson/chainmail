import { graphLanes, type GraphNode } from "../lib/lanes";
import type { Row, View } from "../lib/derive";

const PITCH = 12;
const IND = 11;
const X0 = 11;
const Y0 = 9;
/* keep just enough floor for a single chain lane to stay readable; the svg is
   otherwise sized to its lane count so the overlay panel can hug the tree */
const MIN_W = 96;

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
}

/**
 * A standalone SVG of the whole reply-tree panel — the graph, the tally
 * (chains / lanes / deep / forks / dead ends) and the legend (message, note,
 * starts chain, reconstructed) — with every style baked in, since a downloaded
 * .svg carries no page CSS.
 */
export function treeSvgString(o: TreeExport): string {
  const pal: Palette = PALETTES[o.dark ? "dark" : "light"];
  const byId = new Map(o.nodes.map((n) => [n.id, n]));
  const rowOf = new Map(o.rows.map((r, i) => [r.id, i]));
  const y = (id: string) => Y0 + rowOf.get(id)! * PITCH;
  const x = (id: string) => X0 + byId.get(id)!.lane * IND;

  // same geometry as the on-screen panel: the tree is drawn in its own
  // coordinates then translated so its X0/Y0 margins land on the export's
  const treeW = Math.max(X0 + (o.laneCount - 1) * IND + 13, MIN_W);
  const treeH = Y0 + (o.rows.length - 1) * PITCH + 10;

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
    const x1 = x(n.parent);
    const y1 = y(n.parent);
    const x2 = x(r.id);
    const y2 = y(r.id);
    const attrs = n.isFork
      ? `stroke="${pal.o4}" stroke-width="1.6"`
      : `stroke="${pal.line}" stroke-width="1.3"`;
    const d =
      Math.abs(x1 - x2) < 0.5
        ? `M${x1} ${y1 + 4.4} V${y2 - 4.4}`
        : `M${x1} ${y1 + 4.4} V${y2 - 4.5} Q${x1} ${y2} ${x1 + 4.5} ${y2} H${x2 - 4}`;
    line(`<path d="${d}" fill="none" ${attrs}/>`);
  }
  for (const r of o.rows) {
    const n = byId.get(r.id)!;
    const note = r.entry.kind === "note";
    const cx = x(r.id);
    const cy = y(r.id);
    if (n.isRoot) {
      line(
        `<path d="M${cx - 4.6} ${cy - 6.6} H${cx + 4.6}" stroke="${pal.muted}" stroke-width="1.6" opacity=".85"/>`,
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
 * A sticky index of the reply graph. Down is time — rows follow the transcript's
 * own order, so the panel is row-aligned with the page. Across is only lane
 * allocation: concurrently-live chains, and somewhere for a fork to go.
 */
export function Minimap({ v }: { v: View }) {
  const g = graphLanes(
    v.rows.map((r) => r.entry),
    (e) => v.rows.find((r) => r.entry === e)!.id,
  );
  const byId = new Map(g.nodes.map((n) => [n.id, n]));
  const rowOf = new Map(v.rows.map((r, i) => [r.id, i]));
  const y = (id: string) => Y0 + rowOf.get(id)! * PITCH;
  const x = (id: string) => X0 + byId.get(id)!.lane * IND;

  const width = Math.max(X0 + (g.laneCount - 1) * IND + 13, MIN_W);
  const height = Y0 + (v.rows.length - 1) * PITCH + 10;
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
      <div className="mbody">
        <svg
          width={width}
          height={height}
          viewBox={`0 0 ${width} ${height}`}
          role="img"
          aria-label={`Reply tree of ${v.rows.length} entries, time downward`}
        >
          {/* hit strips first: the hover band paints behind the dots, and .nd/.lk
              are pointer-events:none so the strip always receives the pointer */}
          {v.rows.map((r) => (
            <rect
              key={`hit-${r.id}`}
              className="hit"
              data-id={r.id}
              x={0}
              y={y(r.id) - PITCH / 2}
              width={width}
              height={PITCH}
            >
              <title>
                {`${r.entry.kind === "note" ? r.entry.label : r.entry.sender} — ` +
                  [r.entry.date, r.entry.time].filter(Boolean).join(" ") +
                  (byId.get(r.id)!.isRoot ? " · starts a chain (no parent)" : "")}
              </title>
            </rect>
          ))}

          {v.rows.map((r) => {
            const n = byId.get(r.id)!;
            if (!n.parent) return null;
            const x1 = x(n.parent);
            const y1 = y(n.parent);
            const x2 = x(r.id);
            const y2 = y(r.id);
            const cls = `lk${n.isFork ? " fk" : ""}`;
            const d =
              Math.abs(x1 - x2) < 0.5
                ? `M${x1} ${y1 + 4.4} V${y2 - 4.4}`
                : `M${x1} ${y1 + 4.4} V${y2 - 4.5} Q${x1} ${y2} ${x1 + 4.5} ${y2} H${x2 - 4}`;
            return <path key={`lk-${r.id}`} className={cls} data-c={r.id} d={d} />;
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
            const cx = x(r.id);
            const cy = y(r.id);
            return (
              <g key={`nd-${r.id}`} className={cls} data-id={r.id} data-p={n.parent ?? ""}>
                {n.isRoot ? (
                  <path className="rtcap" d={`M${cx - 4.6} ${cy - 6.6} H${cx + 4.6}`} />
                ) : null}
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
      </div>
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
    </aside>
  );
}
