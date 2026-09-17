import { Fragment } from "react";
import { provenance, type SourceId } from "../lib/sources";

/**
 * The ids on a provenance line, comma-run, each openable where it can be.
 *
 * The separator sits outside .sid so that the only place the line may break is
 * after a comma: inside .sid, "msg" and its handle are one token to the reader
 * and splitting them across lines reads as two truncated ids.
 */
function SourceIds({ ids, unspooled, anchorByGmail }: {
  ids: SourceId[];
  /** the line is "unspooled from …"; its ids name the message the content was lifted out of */
  unspooled: boolean;
  /** gmailId -> this page's anchor for that message, where it is present as a row */
  anchorByGmail: Map<string, string>;
}) {
  return (
    <>
      {ids.map((s, i) => {
        // An unspooled id names a sibling message on this very page, so it links
        // there (a fragment anchor) instead of shipping the reader out to Gmail.
        // A direct message's own id still opens its mailbox copy.
        const anchor = unspooled && s.gmailId ? anchorByGmail.get(s.gmailId) : undefined;
        return (
          <Fragment key={i}>
            {i ? ", " : ""}
            <span className="sid">
              {anchor ? (
                <a href={`#${anchor}`} title="The message this was unspooled from, on this page">
                  {s.text}
                </a>
              ) : unspooled || !s.gmailId ? (
                s.text
              ) : (
                <a
                  href={`https://mail.google.com/mail/u/0/#all/${s.gmailId}`}
                  target="_blank"
                  rel="noopener"
                >
                  {s.text}
                </a>
              )}
            </span>
          </Fragment>
        );
      })}
    </>
  );
}

/**
 * Where an entry was found: `msg <id>`, `unspooled from msg <id>, msg <id>`, or
 * the corpus handle where no mailbox id is known.
 *
 * The whole line, never a disclosure of its own. It is drawn inside the bubble's
 * receipt, and the receipt is already a <details> (see Message): a second one
 * nested in it asked the reader to open the receipt and then open the line to
 * see the ids, which are the one useful thing on it. Opened, the receipt is
 * exactly where a reader wants them — every host this message came out of, in
 * one line that can be read, copied and found by find-in-page.
 */
export function Source({ source, anchorByGmail }: { source?: string; anchorByGmail: Map<string, string> }) {
  if (!source) return null;
  const p = provenance(source);
  if (p.kind === "prose") return <span className="src">{p.text}</span>;
  // "unspooled from …" lines carry an empty prefix only when not unspooled;
  // prose never reaches here, so prefix !== "" means the ids were unspooled
  const unspooled = p.prefix !== "";
  return (
    <span className="src">
      {p.prefix}
      <SourceIds ids={p.ids} unspooled={unspooled} anchorByGmail={anchorByGmail} />
    </span>
  );
}
