# Corpus provenance and verification

What was harvested, how it was partitioned, what the fixtures actually contain,
and — the part that matters most — which sentinel families this mailbox does
**not** contain, so a reader knows which parts of the extractor are untested
rather than unsupported.

## Harvest

`docket mail search` caps `--limit` at 500 with no pagination token, so every
sweep is partitioned by date window and concatenated. Every body was read with
`--max-bytes 99999999`: the default 20000 truncates from the *end* of a body,
which is exactly where the oldest quoted material sits.

| arm | query | windows | distinct ids | read | truncated |
|---|---|---|---|---|---|
| received | `-from:me after:… before:…` | 7 monthly, 2026-02-01 → 2026-08-21 | 1253 | 1253 | 0 |
| received (backfill already on disk) | same, earlier windows | — | 1727 total, 2025-01-02 → 2026-08-20 | 1727 | 0 |
| sent | `from:me after:… before:…` | 16, 2022-01-01 → 2026-08-21 | 300 | 300 | 0 |
| targeted | `"Original Message"`, `"Begin forwarded message"` | none (content queries) | 22 | 8 new | 0 |

**2035 messages read in total.** The received sweep of the recent window found
1253 ids and every one of them was already present in the on-disk harvest — zero
missing — which is what establishes that the received arm is a complete sweep of
the window rather than a sample.

The sent arm reaches back to 2022 because `from:me` returns nothing before
2024-07: 300 messages is *all* the sent mail in the mailbox, not a sample of it.

The targeted sweep exists for one reason. `-----Original Message-----` occurs
**zero** times in all 2027 date-partitioned messages. Three content queries
recovered 7 occurrences across 3 messages. Without them the family would be
absent from the corpus entirely, and the absence would have looked like evidence
that Outlook-style forwards do not happen.

## Fixtures

866 fixtures, 7,950,118 bytes (7.95 MB) including `index.json`.

| arm | fixtures | share |
|---|---|---|
| received | 676 | 78.1% |
| sent (`role` prefixed `sent`) | 190 | 21.9% |

Every message carrying at least one sentinel was committed, plus 4 quoting-free
controls. Two messages were held back:

- **10 dropped for the size budget.** All received, all at depth 0–1, all with at
  most a shallow unwrapped attribution and no wrapped closer, no forward rule and
  no `Begin`/`Original` marker — 38 KB total. Curation ordered by rarity
  signature first, so a budget cut removes the most redundant message rather than
  an only-example.
- **1 rejected outright**, and it is the interesting one. See *Contradictions*.

The original thirteen are kept by name and by role. All thirteen were
**regenerated**, because the first anonymiser's `Name <addr>` pair regex ate their
`From:` / `To:` / `Cc:` header keys — see *Contradictions*.

### Quote depth

| max depth | fixtures |
|---|---|
| 0 | 100 |
| 1 | 233 |
| 2–3 | 220 |
| 4–6 | 145 |
| 7–9 | 64 |
| 10–14 | 35 |
| 15–24 | 43 |
| 25–49 | 14 |
| 50–56 | 12 |

Deepest fixture: 56 levels.

### Sentinel families

Counts are from `stats`, the pre-anonymisation census.

| family | fixtures with ≥1 | occurrences | received fixtures | sent fixtures |
|---|---|---|---|---|
| unwrapped attribution (`On … wrote:`) | 780 | 3282 | 611 | 169 |
| wrapped closer (`wrote:` alone on a line) | 267 | 1061 | 256 | 11 |
| header-key line | 396 | 5781 | 314 | 82 |
| forward rule (`--- Forwarded message ---`) | 291 | 382 | 228 | 63 |
| `Begin forwarded message:` | 27 | 27 | 13 | 14 |
| `-----Original Message-----` | 3 | 7 | 3 | 0 |
| no quoting (control) | 4 | — | 3 | 1 |

The wrapped closer is overwhelmingly a *received* phenomenon: 256 received
fixtures against 11 sent. Wrapping is a function of how deep the prefix already
is when the client hard-wraps at 78 columns, and the sending client's own
attribution is only ever one level deep.

## The sending client

Reported separately from the received distribution, because a received-only
corpus cannot show it. Classified by the *first* sentinel in each sent body —
the one the sending client emitted itself.

Zach sends from **two** clients, and they quote differently.

**Apple Mail (163 of 185 sent messages with a sentinel).** The dominant client.

- Reply attribution: `On 3 Oct 2026, at 09:14, Name <addr> wrote:` — no weekday,
  comma after the year, `at` before the time.
- **The attribution is emitted at quote depth 1, not 0.** 151 of 164 sent replies
  put the client's own attribution *inside* the quote, with the quoted body at
  depth 1 and the next-older nesting at depth 2. Only 13 sent messages have any
  attribution at depth 0 at all. Any extractor that assumes the outermost
  attribution sits at depth 0 will mis-attribute the majority of this mailbox's
  own outgoing replies.
- Forwards: `Begin forwarded message:`, 14 occurrences, **also at depth 1** (12 of
  12 classified cases), followed by a plain header block.
- Header keys are plain. Zero bold-flattened keys.

**Gmail (a small minority, ~9 messages).**

- Forwards: `---------- Forwarded message ---------` at depth **0** (7 of 8) — ten
  leading dashes, nine trailing. One message uses the older symmetric
  `---------- Forwarded message ----------` (ten and ten).
- Two further reply dialects appear, both Gmail:
  - US format, no time at all: `On Tue, Sep 10, 2024, Name <addr> wrote:` (6 messages).
  - Android: `On Sun, 15 June 2026, 14:22 Name, <addr> wrote:` (6 messages) — no
    `at`, and the comma falls **after** the display name rather than before it,
    so the name is not comma-delimited from the address the way every other
    dialect delimits it.

**Both clients:** top-posted, 171 of 185. Header keys always plain — the sent arm
contains **zero** bold-flattened `*From:*` keys and **zero**
`-----Original Message-----` rules. Zach never sends from Outlook.

## Absent from this mailbox

Measured across all 2035 harvested messages. These patterns are *untested* by
this corpus, not unsupported by the parser.

| pattern | occurrences |
|---|---|
| `-----Original Message-----` | 7, and only via a targeted content query |
| bold-flattened header keys (`*From:*`, `*From: *`) | **0** |
| localised header keys (`Von:`/`Gesendet:`/`Betreff:`, `De:`/`Para:`/`Asunto:`, `Objet:`/`Envoyé:`) | **0** |
| non-English closers (`a écrit :`, `schrieb:`, `escribió:`, `ha scritto:`, `escreveu:`, `skrev:`, `napisał:`, `写道：`, `작성:`, `đã viết:`) | **0** |
| non-English openers (`Le`, `El`, `Il`, `Em`, `Op`, `Den`, `På`, `W dniu`, `Vào`, `在`) | 15 lines in 14 messages, every one of them an English word that happens to collide (`Am I …`, `On …`) — **no genuine localised opener** |
| Thunderbird `-------- Original Message --------` | **0** |
| `Sent:` used as an attribution closer | **0** |

So the entire multilingual opener/closer table in `sentinel.go`, and `unbold()`'s
four Outlook dialects, are carried by no fixture here. They may well be correct;
this corpus cannot say.

Present but thin, worth knowing:

| pattern | occurrences |
|---|---|
| bare dash/underscore rules ≥10 chars (Outlook/Teams chrome) | 520 lines in 149 messages |
| `-- ` RFC 3676 signature delimiter | 296 in 272 messages (239 preserved in the fixtures) |
| `[image:` / `[cid:` placeholders | 14 lines in 4 messages |
| `On Behalf Of` in an attribution | 12 messages |

## Anonymisation

Structure is preserved exactly; only identities and prose are replaced. See
`README.md` for the rules. Line separators are re-emitted **verbatim** rather than
normalised: one message in this mailbox is CR CR LF throughout and 45 more mix
bare LF into CRLF, and rewriting those would move the line boundaries the parser
sees — and would invalidate a census taken on the source.

3412 source identities were mapped onto an invented cast (1144 addresses, 336
domains). Any invented name that collided with a real token from the source was
dropped from the cast before mapping began, so the leak audit below can tell an
invented name from a surviving one.

## Leak audit

Run over the concatenated JSON of all 866 fixtures — 7,911,822 bytes, every field,
not just `body` — case-insensitively.

| probe | probed | hits |
|---|---|---|
| every source email address, verbatim | 303 | **0** |
| every source address local part, ≥4 chars, word-bounded | 231 | **0** |
| every source email domain | 105 | **0** |
| every capitalised 2–4 word name run in any source field, word-bounded | 2175 | **0** |
| single-token org names taken from source domain labels, word-bounded | 90 | 1 — see below |
| any URL other than `https://example.fed/x` | — | **0** |
| any currency amount other than `$0.00` | — | **0** |
| any phone-shaped run other than `+61 4 0000 0000` | — | **0** |
| any digit run ≥6 that is not all `9`s | — | **0** |
| 15 known-real literals: the mailbox owner's employer and its former name, three of its clients, four SaaS vendors, three consumer mail domains, two national TLD suffixes, two business-number acronyms. The list is real company names, so it lives in the scratch harness and is **not** committed | 15 | **0** |

The single hit is `outlook`, and it is accounted for: it occurs only in the
*names and roles* of the `outlook-original-message` fixtures and in `index.json`.
It is a mail-client name describing what the fixture exercises, not a source
identity. Nothing in any `body`, `from`, `to`, `cc` or `subject` matches it.

Two leak classes were found by these probes and fixed, having survived every
earlier pass:

1. **An address split across a soft line break.** `… jane.doe@` at the
   end of one line and its domain at the start of the next matches no email
   pattern on either line, so the local part survived intact. Now the dangling
   fragment is mapped while keeping its shape — `local@` stays `local@` and
   `@domain` stays `@domain` — so the wrap point does not move.
2. **A lowercase organisation inside a display name** (`Jane Doe from acmeco`),
   which neither the name-run pass nor the capitalised-token pass can see.

Both are now closed by a denylist final pass: every token that appears anywhere
in the source as an address label or as part of a name is denied outright.

## Census self-check

For every fixture, all seven `stats` fields are recomputed from the committed body
and compared against the stored value, and separately against a census of the
*source* body. **0 drift on 866 fixtures × 7 fields, both ways.**

Note that `corpus_test.go` only asserts `attr`, `wrapped` and `fwd`. The `hdr`,
`begin`, `orig` and `max_depth` fields are checked here, out of band — and it was
the unchecked `hdr` field that hid the header-key bug described below.

## Contradictions found

Three things this corpus says that the README's stated approach did not.

**1. The pair regex ate header keys, and the test suite could not see it.**
`README.md` warns that the `Name <addr>` pair regex must not cross a comma or
contain digits. It must also not cross a **colon**, or `From: Jane <j@x>` matches
from `From` and the header *key* is consumed as part of the display name. All
thirteen original fixtures were damaged; `many-header-blocks` was reduced from 20
header lines to 9 while its `stats` still claimed 20. The census self-check catches
it instantly — but only if it covers `hdr`, which `corpus_test.go` does not. The
regex now also excludes `*`, or Outlook's `*From:*` bold chrome is swallowed the
same way and the flattened-key shape is lost.

**2. One real message defeats `TestFindsEveryHeaderBlockRun` and had to be
rejected.** A single message in this mailbox is CR CR LF throughout. `Normalise`
folds bare CR to a line break, so *every* line of that body ends up separated by
a blank line — and `FindHeaderBlock` breaks on a blank. Its 12 header-key lines
are therefore never adjacent and no header block exists to find, which trips the
test's `hdr >= 4 ⇒ blocks > 0` assumption. The message is real, the census is
right, and the parser is behaving as written; the assumption is what is wrong. It
is excluded from the corpus rather than committed red, and named here so it is not
mistaken for absence of evidence.

**3. `reSigDelim` can never match.** `sentinel.go` defines the RFC 3676 signature
delimiter as `^-- $`, but `Normalise` right-trims trailing spaces and tabs from
every line, so by the time any sentinel pattern runs the line is `--`. There are
296 genuine `-- ` delimiters in the harvest and 239 of them survive in these
fixtures, so the data to test it is here; the pattern as written will find none of
them.
