# Fixtures

Real mail, anonymised. Structure is preserved **exactly** — quote markers and
their depth, sentinel lines, where an attribution wraps, header-block shape,
blank lines and line separators — because structure is what the extractor is
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

Two arms, distinguished by `role`:

- **received** (676 fixtures) carries the client diversity — it is what other
  people's mail clients emit;
- **sent** (190 fixtures, `role` prefixed `sent`) characterises the *sending*
  client, which a received-only corpus cannot show. It is the arm that reveals
  that this mailbox's own client emits its reply attribution at quote depth 1
  rather than 0.

Selected to cover: quote depth up to 56, wrapped attributions (the `wrote:`-alone
case that no surveyed library handles at depth > 0), Apple Mail
`Begin forwarded message:`, Outlook `-----Original Message-----`, multiple forward
rules in one body, bodies with 20+ header-block lines, and flat controls with no
quoting at all.

`CORPUS.md` records the harvest, the family distributions, the leak audit, and —
most importantly — which sentinel families this mailbox does **not** contain, so
a reader knows which parts of the extractor are untested rather than unsupported.

## Rules the anonymiser must not break

Each of these was a real bug that silently invalidated fixtures downstream.

1. **The `Name <addr>` pair regex must not cross a comma, a colon or an
   asterisk, and must not contain digits.** Allowing arbitrary characters before
   the `<` makes `On Tue, 3 Feb 2026 at 07:36, Alice <a@x> wrote:` match from
   `On`, replacing the entire date as though it were a display name; allowing a
   colon makes `From: Jane <j@x>` swallow the header *key*, which destroyed 11 of
   the 20 header lines in the first `many-header-blocks` fixture; allowing an
   asterisk swallows Outlook's `*From:*` bold chrome.
2. **`Subject:` lines are structural but their text is content.** Keep the key and
   its `Re:`/`FW:`/`Fwd:` prefix chain, since the parser tests those, and replace
   the subject text like any prose — keeping it verbatim leaks company names.
3. **An attribution whose `wrote:` sits on a continuation line is still
   structural.** Deciding "structural" from a single line turns a wrapped
   attribution's first line into filler and destroys the exact case the corpus
   exists to cover. The anonymiser mirrors `FindAttribution`'s join to decide.
4. **Line separators are re-emitted verbatim, never normalised.** One message here
   is CR CR LF throughout and 45 more mix bare LF into CRLF; rewriting them moves
   the line boundaries the parser sees and invalidates a census taken on the
   source.
5. **Bodies are CRLF and `$` does not consume the `\r`.** Every anchored pattern,
   in Go and in the anonymiser's own analysis scripts, uses `\r?$`. A census
   anchored on bare `$` reported *zero* attributions across 80 real messages.

## Provenance

Committed as `.txt` so `corpus_test.go`'s `testdata/*.json` glob ignores them:

- `census.py.txt` — the census. Mirrors `corpus_test.go`'s regexes for
  attr/wrapped/fwd/begin/orig, and `line.go`/`sentinel.go` for `max_depth` and
  `hdr`. Parser-independent by construction.
- `anonymise.py.txt` — the anonymiser.
- `validate.py.txt` — Python ports of `FindAttribution` and `FindHeaderBlock`,
  used to predict every assertion in `corpus_test.go` before a fixture is
  committed, so the suite is never left red.
- `build.py.txt` — selection, curation under the size budget, and writing.
- `audit.py.txt` — the leak audit and the seven-field census self-check.
