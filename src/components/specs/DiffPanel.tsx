import type { View } from "../../lib/timeline/derive";

/** What this pass added, relative to the spec recovered from a prior render. */
export default function DiffPanel({
  v,
  marks,
  prevLabel,
}: {
  v: View;
  marks: Map<string, "new" | "revised">;
  prevLabel: string;
}) {
  const pick = (kind: "new" | "revised") => v.rows.filter((r) => marks.get(r.id) === kind);
  const fresh = pick("new");
  const revised = pick("revised");

  const list = (rs: typeof fresh) =>
    rs.map((r) => (
      <li className="my-0.5 text-xs" key={r.id}>
        <a
          className="text-inherit underline decoration-accent decoration-dotted underline-offset-2 hover:text-accent hover:decoration-solid"
          href={`#${r.id}`}
        >
          <b>{r.entry.kind === "note" ? r.entry.label : r.entry.sender}</b>,{" "}
          {[r.entry.date, r.entry.time].filter(Boolean).join(" ")}
        </a>
        {r.entry.source ? (
          <span className="text-[.92em] text-muted">
            {" "}
            {"—"} {r.entry.source}
          </span>
        ) : null}
      </li>
    ));

  if (!fresh.length && !revised.length) {
    return (
      <details className="pan mt-3 rounded-lg border border-line bg-card" open>
        <summary className="cursor-pointer list-none px-3 py-1.5 text-xs font-bold tracking-[.08em] text-muted uppercase hover:text-accent">
          Since last run
        </summary>
        <div className="border-t border-line px-3 pt-0.5 pb-2">
          <div className="srcgrp mt-2 first:mt-0.5">
            <ul className="m-0 list-disc pl-4">
              <li className="my-0.5 text-xs">
                Nothing new. Every entry on this page was already present in {prevLabel}.
              </li>
            </ul>
          </div>
        </div>
      </details>
    );
  }

  return (
    <details className="pan mt-3 rounded-lg border border-line bg-card" open>
      <summary className="cursor-pointer list-none px-3 py-1.5 text-xs font-bold tracking-[.08em] text-muted uppercase hover:text-accent">
        Since last run {"—"} {fresh.length} new, {revised.length} revised
      </summary>
      <div className="border-t border-line px-3 pt-0.5 pb-2">
        {fresh.length ? (
          <div className="srcgrp mt-2 first:mt-0.5">
            <div className="mb-1 text-2xs font-bold tracking-[.09em] text-muted uppercase">
              New since {prevLabel} ({fresh.length})
            </div>
            <ul className="m-0 list-disc pl-4">{list(fresh)}</ul>
          </div>
        ) : null}
        {revised.length ? (
          <div className="srcgrp mt-2 first:mt-0.5">
            <div className="mb-1 text-2xs font-bold tracking-[.09em] text-muted uppercase">
              Revised ({revised.length})
            </div>
            <ul className="m-0 list-disc pl-4">{list(revised)}</ul>
          </div>
        ) : null}
      </div>
    </details>
  );
}
