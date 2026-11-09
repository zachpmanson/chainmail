# Quoted dates and header vocabulary

This covers the fixture-backed parser and safe corpus integration for #377,
not a complete multilingual-client implementation. Fixtures use invented identities and prose.
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

## Corpus time evidence and key continuity

Migration 29 only adds nullable quote time basis/precision and a small validated
key/evidence relation. It does not rewrite legacy bodies, timestamps or external
IDs. NULL basis is unverified legacy state; `inferred` means a host upper bound;
`stated` means a complete parsed wall clock. New extraction sets these explicitly.
The low-level `PutQuoted` compatibility API defaults manually supplied timestamps
to stated/second precision; callers with host bounds must specify inferred.

Fallback/unknown clocks are not twin, derived-copy or offset-measurement evidence,
in either bulk or nearby matching. Legacy address/date keys can independently
prove the stored author/wall clock; legacy Message-ID keys additionally need
bounded, complete agreeing source evidence matching the stored value. Unverified
text keys and surviving inferred markers cannot prove a clock. Authorised apply
sets an explicit stated basis; historical inference sighting detail is retained.
Reconciliation rules/cache versions are bumped, but this **never** authorises an
automatic timestamp repair during slurp, repair, refresh or startup.

A newly understood date can change a parser key from `quote:t:…` to an
address/date key. Stored external IDs remain stable. The compatibility bridge
requires the exact old text key, nonempty identical own body, same resolved
address/person, an existing same-host quoted sighting, proved old inference,
and exactly one matching recovered occurrence in **every** sighting host, with
complete agreeing dates, zones and precision. New-key/alias collisions refuse.
Empty bodies, changed authors, ambiguous repetitions and incomplete/truncated
host evidence cannot bridge. No corpus-wide body search or fuzzy matching occurs.

Normal re-ingestion may reuse that row and record validated aliases, but cannot
promote its stored clock, timezone or time basis. The historical inference marker
survives later `Sight` updates. An unresolved bridge refuses ingestion rather than
creating another dated row. Existing aliases revalidate the complete current
host set, full source digests, sentinel digests, author and key collisions. A
changed date with identical body cannot retarget a previously stated record.
A new, previously unseen host needs separate evidence review rather than an
unvalidated alias extension. Body-only repair honours valid aliases without
changing timestamps; stale aliases leave the entry missing/unmodified. Explicit
twin cleanup transfers alias FKs and retains historical key/digest/host provenance;
a moved host's old digest is not replaced with its survivor's content.

## Explicit bounded local reparse

No provider is involved. Back up the source using your normal consistent SQLite
backup procedure. Preview accepts **only an explicitly frozen standalone snapshot**,
not a live database. Create a private consistent copy with SQLite's backup API
(which includes committed WAL data), or copy a quiescent, cleanly closed database
with no companions. Do not copy just the main file of a live WAL database, delete
sidecars to force acceptance, or chmod the production database.

For example, with private paths selected by the operator:

```sh
# SOURCE_DB is the original corpus; SNAPSHOT_DB is a new private copy.
# SQLite .backup includes committed WAL; a plain live-file cp does not.
(umask 077; sqlite3 "$SOURCE_DB" ".backup '$SNAPSHOT_DB'")
chmod 0400 "$SNAPSHOT_DB" # private snapshot only, never the source
CHAINMAIL_CORPUS="$SNAPSHOT_DB" corpus reparse-quotes -entry quote:t:SYNTHETIC -out reviewed-plan.json
# Or one container, with deterministic entry-ID paging:
CHAINMAIL_CORPUS="$SNAPSHOT_DB" corpus reparse-quotes -container synthetic-thread -limit 100 -out batch-1.json
# Use the reported NextID with -after-id for a subsequent preview.
```

The shared `OpenReadOnly` helper is a frozen-snapshot reader, not a general live
readonly connection. It requires a regular non-symlink main file with **no write
permission bits** (0400 recommended) and no `-wal`, `-shm` or `-journal` companion,
including empty companions. Writable/live inputs refuse; permissions are never
changed automatically. Keep the file and its private directory frozen for the
handle's entire lifetime; the checks are not a lock against concurrent file
replacement or an owner re-enabling writes. Only after those checks does SQLite
open with `immutable=1`, `mode=ro` and `query_only`, so no sidecars are created even
while the reader is open. It performs no migrations, WAL setup, cache publication,
sightings or FTS writes. It supports schema 28 (pre-evidence) through the current
known schema; unsupported versions refuse. It does not create a missing database
or parent directory. Whole-host reads are bounded, including hosts outside the
selected container: at most 100 entries, 1,000 distinct hosts and 32 MiB source
text/HTML (including selected own-body reads). Lower these with `-limit`, `-max-hosts`, `-max-bytes`; raise none beyond
the hard maxima. Oversized hosts are not truncated and parsed; incomplete budgets
report `evidence_limit` and cannot propose partial-consensus updates. Plans expose
limits, loaded-host/byte counts, skips/reasons, continuation and metadata changes.
They contain local IDs, old/new wall clocks and evidence digests, not message
bodies, subjects, raw headers, addresses or account IDs. Keep plans private.

Review the exact JSON, especially Changes and Skipped, and separately authorise:

```sh
# Separately authorised write against the source, not the frozen snapshot:
CHAINMAIL_CORPUS="$SOURCE_DB" corpus reparse-quotes -apply -plan reviewed-plan.json
# Create a fresh frozen snapshot and re-preview afterward (expect zero changes).
```

Apply requires the exact serialized plan/digest/parser version, not a selector.
The JSON digest detects edits; it is **not** a signature or authorisation service.
The CLI rejects unsupported schemas before writable journal setup or migration;
known schema-28 migration remains supported without automatic metadata backfill.
Planning and apply also recheck the supported schema range in their transactions.
Under the existing short writer lock, apply checks the source revision and
recomputes the exact bounded proposal, including current rows, aliases, all
source/sentinel digests, author resolution, sightings and mailbox authority.
Any change refuses atomically; it never silently replans into a different write.
A schema-28 reviewed plan can be applied after normal schema migration, which
itself changes no source metadata. Reapplying an old applied plan is a stale
refusal; a fresh already-satisfied preview applies as a zero-write no-op.

Only inferred/proved-inferred legacy quotes are eligible, and only for a complete
stated timestamp and nonconflicting timezone. All hosts must agree on the stated
clock as well as key, zone and precision: a shared Message-ID is not proof of an
identical date. Already stated, derived, ambiguous,
mailbox-promoted or mail-detail/account-copy-authoritative rows refuse. Apply
changes only selected quoted `ts`, `tz`, basis, precision and validated aliases.
It leaves `tz_offset` unknown and never invents a server/person offset. It does
not touch original text/HTML, mailbox times, body hashes, embeddings, participants,
sightings/history, parents, account copies or external IDs; it performs no merges.
Ordinary `repair` remains a separate body/identity operation, not metadata apply.
Run your usual foreign-key, SQLite and both external-content FTS integrity checks
after authorised apply. The synthetic regression suite verifies these alongside
protected-table snapshots, stale refusals, key continuity and repeated application.

**The original attribution-only blank wrapper remains ambiguous and retained.**
The nested Italian date improves metadata/order, but supplies neither missing
wrapper subject nor recipient identity evidence. A full-header variant with
affirmative evidence still reconciles under the existing guards; resolving the
missing-evidence variant requires a separate evidence-backed change.
