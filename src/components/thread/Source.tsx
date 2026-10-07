import { Fragment } from "react";
import { provenance, type SourceId } from "../../lib/message/sources";
import { gmailMessageURL } from "../../lib/message/gmailUrl";

/** The separator sits outside each nowrap span so the line only breaks after a comma. */
function SourceIds({
  ids,
  unspooled,
  anchorByGmail,
}: {
  ids: SourceId[];
  /** the line is "unspooled from …"; its ids name the message the content was lifted out of */
  unspooled: boolean;
  /** gmailId -> this page's anchor for that message, where it is present as a row */
  anchorByGmail: Map<string, string>;
}) {
  return (
    <>
      {ids.map((s, i) => {
        // An unspooled id links to its sibling on this page rather than out to Gmail.
        const anchor = unspooled && s.gmailId ? anchorByGmail.get(s.gmailId) : undefined;
        return (
          <Fragment key={i}>
            {i ? ", " : ""}
            <span className="whitespace-nowrap">
              {anchor ? (
                <a
                  className="text-inherit no-underline hover:text-accent hover:underline"
                  href={`#${anchor}`}
                  title="The message this was unspooled from, on this page"
                >
                  {s.text}
                </a>
              ) : unspooled || !s.gmailId ? (
                s.text
              ) : (
                <a
                  className="text-inherit no-underline hover:text-accent hover:underline"
                  href={gmailMessageURL(s.gmailId)}
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

/** Where an entry was found: `msg <id>`, `unspooled from msg <id>, …`, or the corpus handle. */
export default function Source({
  source,
  anchorByGmail,
}: {
  source?: string;
  anchorByGmail: Map<string, string>;
}) {
  if (!source) return null;
  const p = provenance(source);
  if (p.kind === "prose")
    return <span className="ml-auto font-mono text-2xs text-muted">{p.text}</span>;
  // Prose never reaches here, so a non-empty prefix means the ids were unspooled.
  const unspooled = p.prefix !== "";
  return (
    <span className="ml-auto font-mono text-2xs text-muted">
      {p.prefix}
      <SourceIds ids={p.ids} unspooled={unspooled} anchorByGmail={anchorByGmail} />
    </span>
  );
}
