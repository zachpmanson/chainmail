import type { RowEdit } from "../lib/edits";

const html = (s: string) => ({ __html: s });

/**
 * A quoter's inline edit to a message this one quoted (issue #42): the modified
 * text with the change marked, anchored to the original, attributed to the
 * quoter — drawn inside the message that made it rather than leaving the derived
 * copy to float as its own node.
 *
 * One component because two renderers draw it from two holdings: a built page
 * resolves the edit against the spec's rows, the reading pane against the corpus
 * entries it was handed, and `resolveEdits` (lib/edits) reduces both to this. The
 * words, the markup and the anchor are therefore the same in both, and only the
 * id space they were resolved in differs.
 */
export function Edits({
  edits,
  fallbackWho,
}: {
  edits?: RowEdit[];
  /** who to credit when the edit itself named nobody — the page's title, in
   *  practice, which is its last honest answer. A pane's edit always carries the
   *  quoting message's sender, so it never reaches this. */
  fallbackWho?: string;
}) {
  if (!edits?.length) return null;
  return (
    <div className="edits">
      {edits.map((ed, i) => (
        <div className="edit" key={ed.base || i}>
          <div className="ehdr">
            edited by <span className="editwho">{ed.who || fallbackWho || "someone"}</span>
            {ed.origWho || ed.origStamp ? (
              <>
                ,{" "}
                <a href={`#${ed.base}`} title="the message this change was made to">
                  original
                </a>
                {ed.origWho ? <span> from {ed.origWho}</span> : null}
                {ed.origStamp ? <span className="ets"> at {ed.origStamp}</span> : null}
              </>
            ) : null}
          </div>
          <div className="ebd" dangerouslySetInnerHTML={html(ed.html)} />
        </div>
      ))}
    </div>
  );
}
