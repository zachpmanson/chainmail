# Replaced quote links

`replaced-links.json` anonymises two stored mailbox/quote body pairs from an
owner-reported duplicate thread. Names, organisations, business identifiers,
dates and all URL hosts/paths/tokens have been replaced. No original IDs or
private URL tokens are retained.

The fragments preserve the authored paragraphs, CRLF/LF differences, blank-line
layout, link position (including a final link), and 12/13-hour quoting offsets.
Mailbox history is omitted here; integration tests add a synthetic quote trail.
These are stored-text-derived cases, not exports of the rendered HTML endpoint.
The first message is below the usual 25-word floor once URL tokens are removed.

A security gateway replaces a spreadsheet URL without changing the surrounding
prose. The changed URL must remain available as an annotated quote, not be
silently merged away or shown as an unrelated duplicate.
