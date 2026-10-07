import { ArrowDownTrayIcon } from "@heroicons/react/24/outline";
import { graphLanes } from "../../lib/timeline/lanes";
import type { View } from "../../lib/timeline/derive";
import { prefersDark } from "../../lib/ui/palette";
import {
  STEP,
  capD,
  linkD,
  pos,
  size,
  treeFileName,
  treeSvgString,
  type Orient,
} from "../../lib/timeline/treeSvg";

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
    a.download = treeFileName(v.title);
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
    <div className="foot2 flex flex-col gap-1 border-t border-line px-2 pt-1 pb-1.5 text-2xs leading-tight text-muted">
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
          <svg className="size-[.7em] shrink-0" viewBox="0 0 10 10" aria-hidden="true">
            <circle cx="5" cy="5" r="2.9" fill="currentColor" />
          </svg>
          <dt className="m-0 text-muted">message</dt>
        </div>
        <div className="flex items-center gap-1.5">
          <svg className="size-[.7em] shrink-0" viewBox="0 0 10 10" aria-hidden="true">
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
          <svg className="size-[.7em] shrink-0" viewBox="0 0 10 10" aria-hidden="true">
            <path d="M2 2.6 H8" stroke="currentColor" strokeWidth="1.1" />
            <circle cx="5" cy="5.5" r="2.5" fill="currentColor" />
          </svg>
          <dt className="m-0 text-muted">starts thread</dt>
        </div>
        <div className="flex items-center gap-1.5">
          <svg className="size-[.7em] shrink-0" viewBox="0 0 10 10" aria-hidden="true">
            <circle cx="5" cy="5" r="2.9" fill="none" stroke="currentColor" strokeWidth="1.2" />
          </svg>
          <dt className="m-0 text-muted">reconstructed</dt>
        </div>
      </dl>
    </div>
  );

  return (
    <aside
      className="mini fixed inset-y-0 right-0 z-30 flex w-max max-w-[16.5rem] flex-col border-l border-line bg-card print:hidden max-[1024px]:top-[2.4rem] max-[1024px]:bg-card/86"
      id="mini"
    >
      <h3 className="m-0 flex items-center gap-1.5 border-b border-line px-3 pt-2 pb-1.5 text-2xs font-bold uppercase tracking-[.09em] text-muted">
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
