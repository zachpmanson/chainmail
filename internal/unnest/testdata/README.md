# Fixtures

Real received mail, anonymised. Structure is preserved **exactly** — quote markers
and their depth, sentinel lines, where an attribution wraps, header-block shape,
blank lines and CRLF line endings — because structure is what the extractor is
tested against. Only identities and prose are replaced:

- display names and addresses map consistently to an invented cast, with a
  `Name <addr>` pair always drawn from the same key so the two agree;
- prose lines become filler of the same word count;
- URLs, phone numbers, currency amounts and long digit runs are masked;
- **dates are kept verbatim**, deliberately: their real formats are exactly what
  attribution parsing has to survive, and they are not sensitive.

`stats` on each fixture is an independent census of sentinel lines, computed by
regex over the source *before* anonymisation. The tests assert the extractor finds
at least that many boundaries, so the census is a recall floor the parser cannot
quietly fall below.

Selected to cover: quote depth up to 18, wrapped attributions (the `wrote:`-alone
case that no surveyed library handles at depth > 0), Apple Mail
`Begin forwarded message:`, Outlook `-----Original Message-----`, multiple forward
rules in one body, bodies with 20+ header-block lines, and a flat control with no
quoting at all.
