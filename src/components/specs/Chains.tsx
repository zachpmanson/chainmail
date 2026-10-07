import type { View } from "../../lib/timeline/derive";

export default function Chains({ v }: { v: View }) {
  return (
    <>
      {v.layout.chains.map((c) => (
        <div
          key={`spine-${c.root}`}
          className="spine"
          style={{ gridColumn: c.lane + 1, gridRow: `${c.firstRow}/${c.lastRow + 1}` }}
        />
      ))}
      {v.layout.chains.map((c) => (
        <div
          key={`sec-${c.root}`}
          className="chsec"
          style={{ gridColumn: c.lane + 1, gridRow: `${c.firstRow}/${c.lastRow + 1}` }}
        >
          <div className="chdr" title={v.whoTitle(c.opener)}>
            <b>{c.subject ?? c.opener}</b>
            <span>
              {c.subject ? `${c.opener} · ` : ""}
              {c.date} · {c.entries.length} message{c.entries.length === 1 ? "" : "s"}
            </span>
          </div>
        </div>
      ))}
    </>
  );
}
