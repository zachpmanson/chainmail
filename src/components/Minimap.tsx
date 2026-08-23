import { graphLanes } from "../lib/lanes";
import type { View } from "../lib/derive";

const PITCH = 12;
const IND = 11;
const X0 = 11;
const Y0 = 9;
const MIN_W = 232;

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

  return (
    <aside className="mini" id="mini">
      <h3>
        Reply tree<span className="ct">{v.rows.length}</span>
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
        {g.roots} <b>chain</b> · {g.laneCount} <b>lane</b> · {deepest} <b>deep</b> · {g.forks} <b>fork</b>
      </div>
    </aside>
  );
}
