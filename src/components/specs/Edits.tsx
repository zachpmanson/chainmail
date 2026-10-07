import type { RowEdit } from "../../lib/timeline/edits";
import { html } from "../../lib/ui/html";

/** A quoter's inline edit to a quoted message (issue #42), drawn inside the quoting message. */
export default function Edits({ edits, fallbackWho }: { edits?: RowEdit[]; fallbackWho?: string }) {
  if (!edits?.length) return null;
  return (
    <div className="mt-1.5">
      {edits.map((ed, i) => (
        <div className="rounded-md border-l-2 border-line bg-dash px-2 py-1" key={ed.base || i}>
          <div className="text-xs text-muted">
            edited by <span>{ed.who || fallbackWho || "someone"}</span>
            {ed.origWho || ed.origStamp ? (
              <>
                ,{" "}
                <a
                  className="text-accent"
                  href={`#${ed.base}`}
                  title="the message this change was made to"
                >
                  original
                </a>
                {ed.origWho ? <span> from {ed.origWho}</span> : null}
                {ed.origStamp ? <span className="ml-1.5"> at {ed.origStamp}</span> : null}
              </>
            ) : null}
          </div>
          <div className="overflow-x-auto text-sm" dangerouslySetInnerHTML={html(ed.html)} />
        </div>
      ))}
    </div>
  );
}
