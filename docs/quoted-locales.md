# Quoted dates and header vocabulary

This is **parser phase 1 of #377**, not an existing-corpus repair or a complete
multilingual-client implementation. Fixtures use invented identities and prose.
They demonstrate supported syntax, not that a particular client/version emitted
that synthetic example. The English client shapes also have existing anonymised
regression fixtures.

## Supported format matrix

All positive rows traverse `Normalise → Peel → Parse` in
`internal/unnest/testdata/localized/formats.json` (`TestLocalizedPipeline`).
Dates require a year, calendar day and complete clock. Named months remove numeric
order ambiguity; an opener or sender's identity never selects a numeric order.

| Language/shape           | Attribution shape exercised                                      | Date grammar exercised                                 | Fixture name           |
| ------------------------ | ---------------------------------------------------------------- | ------------------------------------------------------ | ---------------------- |
| English Gmail en-GB      | `On …, Alex … wrote:`                                            | `Mon, 5 Oct 2026 at 22:07`                             | English en-GB          |
| English Gmail en-US      | `On …, Alex … wrote:`                                            | `Mon, October 5, 2026 at 10:07 PM`                     | English en-US          |
| English Apple            | `Begin forwarded message:` plus headers                          | `5 October 2026 at 10:07:00 PM NZDT`                   | English Apple          |
| English Outlook          | `From` / `Sent` / `Subject`                                      | `Monday, 5 October 2026 22:07`                         | English Outlook        |
| Italian observed forward | English dashed forward rule plus `Da` / `Date` / `Subject` / `A` | **`lun 5 ott 2026, 22:07`**                            | Italian observed       |
| Italian attribution      | `Il … ha scritto:`                                               | `lun. 5 ottobre 2026 alle 22:07`                       | Italian attribution    |
| French                   | `Le … a écrit :`                                                 | `lun. 5 oct. 2026 à 22:07`                             | French                 |
| German                   | `Am … schrieb:`; `Von` / `Gesendet` / `Betreff`                  | `Montag, 5. Oktober 2026 um 22:07` (headers omit `um`) | German; German headers |
| Spanish                  | `El … escribió:`                                                 | `lunes, 5 de octubre de 2026 a las 22:07`              | Spanish                |
| Portuguese               | `Em … escreveu:`                                                 | `segunda-feira, 5 de outubro de 2026 às 22:07`         | Portuguese             |
| Dutch                    | `Op … schreef:`                                                  | `maandag 5 oktober 2026 om 22:07`                      | Dutch                  |
| Danish                   | `Den … skrev:`                                                   | `mandag 5. oktober 2026 kl. 22:07`                     | Danish                 |
| Norwegian                | `Den … skrev:`                                                   | `mandag 5. oktober 2026 kl. 22:07`                     | Norwegian              |
| Swedish                  | `Den … skrev:`                                                   | `måndag 5 oktober 2026 kl. 22:07`                      | Swedish                |
| Polish                   | `W dniu … napisał:`                                              | `poniedziałek, 5 października 2026 o 22:07`            | Polish                 |

These are grammar families, **not universal language support**. Within each
lexicon in `internal/unnest/date.go`, the supported named-month form is
`[weekday,] day month year [joiner] clock`. English also supports
`[weekday,] month day, year [at] clock`. Spanish and Portuguese additionally allow
the explicit `day de month de year` form. Weekdays/joiners are optional; when
stated and recognised, a weekday must agree with the date. Each lexicon is tried
as a whole, not by replacing month-like words throughout a sentinel.

`TestDateLexiconsThroughHeaderPipeline` checks every listed month/weekday spelling,
uppercase variants and trailing abbreviation dots through the full header
pipeline. `TestDateDiagnosticsAndPrecision` covers ISO `YYYY-MM-DD clock`, English
`Sept`, 12/24-hour clocks, optional seconds, Unicode spaces, decomposed accents,
explicit offsets and unknown zone labels. The existing English corpus suites
retain their recall floors. A baseline comparison finds one intentional date-count
change: 5,372 to 5,371 stated dates among 5,374 attributions. The existing malformed
`16:33 PM` specimen previously became midnight; it now reports `invalid_clock`.
No previously complete valid attribution time changed; header date recall remains
1,294 of 1,295. `TestMalformedExistingEnglishClockIsNotStated` makes the safety
change explicit. Month/weekday spellings not listed in the lexicons,
other numeric orders, relative dates and additional client word orders are not
promised. ISO support here is the date followed directly by a clock, not arbitrary
ISO timestamp syntax.

### Existing boundary-only vocabulary

The pre-existing opener/closer vocabulary is retained in `locales.go`; the lists
are not language-paired and do not prove a date format is supported.
`TestExistingDetectionOnlyLanguages` exercises these limitations explicitly:

| Existing token                | Current status                                                                                                               |
| ----------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Vietnamese `Vào` / `đã viết:` | Boundary recognised; Vietnamese named/numeric date grammar unsupported.                                                      |
| Romanian `Pe`                 | Opener only. Native `a scris:` is unsupported; a mixed English closer can detect a boundary but not parse the Romanian date. |
| Finnish `kirjoitti:`          | Closer only, no native opener. Mixed English opener can detect an undated boundary.                                          |
| Chinese `在` / `写道：`       | Spaced opener form recognised; Chinese date grammar unsupported. No claim for unspaced/CJK punctuation variants.             |
| Korean `작성:`                | Closer only, no native opener/date grammar. Mixed English opener can detect an undated boundary.                             |

Other retained opener alternatives are `på`, `dnia`; retained closer alternatives
include `sent:`, `écrit:`, `scritto:`, `napisała:` and `pisze:`. The original blank
line, wrap bound and second-opener guards still apply. Named forward separators
remain the existing English dashed `Forwarded message`, dashed `Original
Message`, and `Begin forwarded message:` forms. Other translated separator
spellings are **not** claimed; a recognised header block below one may still be
detected independently. Bare rules require corroborating headers/attribution.
A separator that consumes an attribution now uses attribution parsing without
changing its `KindForwardRule` structural classification.

## Header aliases: one lookup for detection and extraction

`parseHeaderLine` is the exact, case-insensitive label lookup used throughout
header detection, folding and parsing. Labels accept Unicode NFC and Outlook's
HTML-to-text `*Label:*` / `*Label*:` variants. Date tokens also accept NFC and
ordinary/non-breaking/narrow spaces; authors' display names are not transliterated
or NFC-rewritten. Original stored text/HTML is not changed by this parser.

| Field                       | Accepted labels                                                                             |
| --------------------------- | ------------------------------------------------------------------------------------------- |
| Sender                      | `From`, `Von`, `De`, `Da`, `Van`, `Fra`                                                     |
| Date                        | `Sent`, `Date`, `Gesendet`, `Enviado`, `Envoyé`, `Verzonden`, `Inviato`, `Sendt`, `Skickat` |
| To                          | `To`, `An`, `Para`, `Aan`, `Till`, **`A`**                                                  |
| Subject                     | `Subject`, `Betreff`, `Asunto`, `Objet`, `Oggetto`, `Ämne`, `Assunto`, **`Onderwerp`**      |
| Cc                          | `Cc`, `Bcc` (union, not last-value-wins)                                                    |
| Message ID                  | `Message-ID`                                                                                |
| Recognised but not a sender | `Reply-To`                                                                                  |

Every alias is exercised with a companion sender through the full pipeline by
`TestEveryHeaderAliasThroughPipeline`, including uppercase/bold/accented labels
and quote depth. This is an audit of the existing vocabulary, not an exhaustive
translation table: e.g. French `À` and Swedish `Från` are not added implicitly.

Detection still requires at least two nonempty keys, including an identity-bearing
non-recipient field; `Reply-To` is not sender evidence. Unknown labels do not
become routing values. Recipient-only emphasis, meeting chrome and prose-like
inline attributions with incomplete/invalid dates must not fabricate messages.
Folds are bounded; recipient tails retain their existing mid-list checks, and a
final date fold must complete a supported grammar. Dates are parsed only after
logical values are assembled. Conflicting repeated Date/Sent fields yield no
stated time; matching repeated dates can retain explicitly stated seconds.

## Date result and inspection

`DateParserVersion` is **1**. `ParseDate(string)` validates an entire quoted
Date/Sent value and returns `DateResult{Sent, TZ, Precision, Reason}`.
`Attribution`, `HeaderBlock` and `Recovered` retain their existing `Sent`/`TZ`
fields and additionally expose `DatePrecision` and `DateReason` strings.
`SplitWhen` remains a compatibility clock-pivot wrapper: its boolean means that
a clock-looking token was found, **not** that a complete date parsed.

- `Sent` is UTC-encoded **wall time**, not a timezone-converted instant. Missing,
  malformed, incomplete and ambiguous values remain zero. Never substitute
  midnight when the stated clock fails.
- `Precision` is `unknown`, `minute` or `second`. A minute value's zero seconds
  do not assert second precision; no current year/day/clock is supplied.
- `TZ` preserves a stated uppercase 2–5-letter label or validated `+HHMM` /
  `+HH:MM` numeric offset. An unknown label is not a failed wall-clock parse and
  is not resolved to an offset. An absent zone remains empty. Neither server
  timezone nor the author's identity supplies one. Comma-separated author names
  must not become zone labels. Numeric hours must be ≤23 and minutes ≤59.
- No change is made to `TZOffset`, render offsets or issue #148 semantics.

Stable reasons (no raw header values, bodies or addresses):

| Code                      | Meaning                                                                                                              |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `parsed`                  | Complete stated date and clock.                                                                                      |
| `missing_date`            | No date supplied.                                                                                                    |
| `missing_clock`           | A recognised complete calendar date lacks a clock.                                                                   |
| `incomplete_date`         | Recognised date tokens lack required components, e.g. year.                                                          |
| `ambiguous_numeric_date`  | Numeric order is not supported/resolved; even unambiguous non-ISO numeric forms are conservatively unsupported here. |
| `unsupported_date_format` | No fixture-backed grammar, or extra material in a whole header value.                                                |
| `invalid_date`            | Impossible calendar fields.                                                                                          |
| `invalid_clock`           | Malformed, out-of-range or glued clock token; no substring/midnight fallback.                                        |
| `invalid_zone`            | Malformed or out-of-range explicit numeric offset.                                                                   |
| `contradictory_date`      | Recognised weekday mismatch or conflicting repeated Date/Sent evidence.                                              |

`corpus unnest <local-entry-id>` prints a metadata line per boundary, for example:

```text
date: stated  precision: minute  reason: parsed  parser: 1
```

This local inspection command already explicitly displays source sentinels/body
excerpts (and `-full` displays more). The added diagnostic line contains only
state/precision/reason/version, and introduces no ingestion telemetry or private
body logging. Default nesting/source ranges and optional `-chrono` inspection
remain available.

## Scope and existing-corpus warning

The synthetic Italian standalone and attribution-only nested wrapper now recover
exactly **2026-10-05 22:07**, minute precision, no zone. Mixed English/Italian/French
nested tests retain own bodies, authors, depth and ordering. **The original
attribution-only blank wrapper remains ambiguous and retained**: parsing the
nested date does not supply missing wrapper subject/recipients or prove identity.
No identity, HTML-conflict, twin, repair or reconciliation guards are relaxed.

This phase does **not** migrate, reparse or update existing records. Newly parsed
dates can change parser-produced address/date keys from the old text-hash keys;
the key algorithm itself is unchanged. Existing body-only repair matches current
keys and cannot safely repair those historical metadata/key differences. Normal
re-ingestion is **not** an approved metadata migration and can create another
recovered row for affected legacy entries. A stored host-bound fallback timestamp
must not be promoted to stated matching evidence just because this parser now
recognises a date elsewhere.

Deployment/integration is gated on the separately reviewed existing-corpus slice:
bounded read-only preview, validated old/new key continuity, explicit inferred vs
stated time basis, separately authorised metadata-only apply, idempotence and
provenance/integrity checks. No persistence migration, alias write, provider call,
corpus repair, automatic refresh authorisation or production apply is included in
phase 1. A parser version change alone authorises none of those operations.
