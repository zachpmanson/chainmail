import { Fragment } from "react";
import { COLLAPSE_FROM, msgCount, provenance, type SourceId } from "../lib/sources";

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
 * Where an entry was found. The ids are the useful part of the line — each names
 * a message the reader can open — so a collapsed line says how many there are
 * and keeps every id in the document, rather than summarising them away.
 *
 * A native <details>, matching the panels above, and not a scripted toggle: the
 * exported page is meant to be readable with scripting disabled, and <details>
 * is keyboard-operable and reachable by find-in-page without any of ours. A
 * folding mechanism elsewhere on the page can be the same element.
 */
export function Source({ source, anchorByGmail }: { source?: string; anchorByGmail: Map<string, string> }) {
  if (!source) return null;
  const p = provenance(source);
  if (p.kind === "prose") return <span className="src">{p.text}</span>;
  // "unspooled from …" lines carry an empty prefix only when not unspooled;
  // prose never reaches here, so prefix !== "" means the ids were unspooled
  const unspooled = p.prefix !== "";
  const ids = <SourceIds ids={p.ids} unspooled={unspooled} anchorByGmail={anchorByGmail} />;
  if (p.ids.length < COLLAPSE_FROM) {
    return (
      <span className="src">
        {p.prefix}
        {ids}
      </span>
    );
  }
  return (
    <details className="src srcx">
      <summary>
        {p.prefix}
        {msgCount(p.ids.length)}
      </summary>
      <div className="srcids">{ids}</div>
    </details>
  );
}
