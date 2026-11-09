# Received reactions

Initial support is **Gmail JSON payload v1**, not RFC 9078. It depends on docket
`github.com/zachpmanson/docket/gmail v0.0.0-20261109103945-12552c23036b`
(the reviewed dependency commit). No new provider account-selection/auth APIs or
outbound reacting are added.

## Recognition and provenance

Both the library conversion and docket CLI JSON retain nullable
`reaction_inspection`. Classification calls docket's public
`ValidateReactionInspection` again, including after manifest deserialization.
**Inspection contract v2** is required; its Gmail payload is still **version 1**.
Inspection v1 cannot establish fallback completeness and is ordinary mail.
The adapter validates exact fully-qualified Unicode Emoji 16.0 sequences, without
normalization: VS16, modifiers, flags and ZWJ sequences survive unchanged.
Unsupported versions, RFC9078, malformed/conflicting/incomplete markers, unknown
layouts, mixed MIME, incomplete fallback leaves and body-only lookalikes remain
ordinary visible messages. There is no language/prose heuristic.

A candidate needs one explicit, matching In-Reply-To and a strict top-level or
alternative layout, with no attachment metadata contradicting that assertion.
Structural evidence is sender supplied, **not proof of authenticated intent**.
The synthetic fixture `testdata/gmail-reaction-synthetic.json` combines the
observed style of decoded presentation with invented MIME evidence. It does not
verify the owner's actual notification; authorized real-source verification is
still pending.

Migration 30 appends evidence, source-event, exact-reference and apply-audit
sidecars. Original entry bodies/HTML, Message-ID, timestamps, mailbox labels and
source records are not rewritten to produce chips. Each account/provider copy
retains inspection JSON, inspection digest and an exact source-snapshot digest.
Repeated identical observations are no-ops. Divergent observations of the same
source identity remain conflicts, including content changes from an old decoder;
a later repeat cannot erase them. Missing old metadata is not an inspected-none
claim. Confirmed structural candidates bypass quoted extraction even when their
target is missing. Existing source-origin quote evidence is not auto-withdrawn.

Association uses only exact Message-ID references and a common stored Gmail
account/copy. References, parent/thread position, subject and quoted prose never
supply a target. Missing, inaccessible, ambiguous and self references retain the
normal source card; late targets resolve through the same view. A target that is
itself an agreed reaction candidate is not a chip host (including cycles).
Exact reference aliases follow target absorption; source notification identities
are protected from body/sender twin merges. Copy/target deletion cannot leave a
stale hidden source. The inherited one-copy-per-logical-entry-per-account
constraint is unchanged: conflicting copy remaps refuse rather than drop copies.

## Display, search and counts

Each exact emoji group counts **distinct source notification IDs**, not distinct
people and not a provider's current reaction state. Copies and repeat syncs do
not inflate it; separate notifications by the same From remain separate events.
Native details/summary show claimed From, source Date, source identity, original
notification text and a Gmail source link, in the shared browser/static Message
component. There is no hover-only attribution or client-side classification.
Direct source lookup and original HTML inspection remain available.

Only active, unambiguous, resolved, reaction-only events leave ordinary authored
search results (prose, identifier, semantic), snippets, titles, chain/people
message counts and fold evidence. Source events are excluded from identity-
destroying twin heuristics even while unresolved. Source text is still stored
and indexed for inspection; search eligibility is a separate predicate. Eligibility
changes invalidate fold membership and vectors independently of BodySHA. Target
ordinary content is searchable once. Ambiguous/mixed/unresolved mail remains
ordinary. Corpus/source totals and provider label/unread counts still include
notification sources; unread is not an authored-message count and may exceed it.
Accessible reaction sources nominate their target conversation for structural
mailbox browsing even when only the source is in INBOX/UNREAD. Their timestamps
contribute to latest activity, ordering and pagination, not authored snippets.
No read/unread or label changes happen as a display side effect.

`accountId` scopes chain reads, direct source evidence, page builds and reaction
association. Saved `runParams.accountId` survives refresh. Quotes independently
sighted through selected-account hosts remain eligible without direct copies.
Selected-account copies supply Gmail IDs/permalinks. Account-local thread IDs
are not retained per copy, so scoped exports omit unproven legacy thread IDs
and link through the selected message copy instead. Quoted-only views rederive
text and headers from selected-account host evidence rather than globally enriched
bodies, recipients or subjects; unprovable projections show unavailable content.
Host provenance and HTML recovery use only selected-account hosts. Unavailable
quotes are placed at the earliest accessible host timestamp with an explicit
original-date/placement caveat; that placement is never persisted. Known mailbox
emails select Gmail links with `authuser`; absent emails retain generic legacy
links without guessing a browser account index.
Scoped refresh cannot use the legacy provider-fetch path: ingest that account
separately, then refresh from the corpus. Unscoped legacy behavior is retained.
These are local account/copy boundaries, not a new server authentication system.

## Existing corpus: explicit preview, then reviewed apply

There is no backfill in startup or slurp. Take a consistent private SQLite
backup/snapshot first. If its schema is old, migrate a writable private replica
with `corpus init`, then close and freeze that replica to mode 0400. Do not
migrate or chmod production merely to prepare a preview. Preview
refuses writable inputs or any -wal/-shm/-journal companions, and opens with
immutable=1, mode=ro and query_only; it never checkpoints, chmods or migrates the
live corpus. An old/future snapshot schema is a refusal, not migration.

```
umask 077
CHAINMAIL_CORPUS=<frozen-private-snapshot> corpus reactions -account <account-id> -ids <gmail-source-id,...> -out PRIVATE-review.json > summary.json
# Inspect exact source/evidence/target/status/refusals locally, then separately:
CHAINMAIL_CORPUS=<live-corpus> corpus reactions -apply PRIVATE-review.json
```

Preview permits 1–20 distinct explicit IDs already stored in the selected
account and uses only the supported Mailbox.Read boundary. It does not search or
refetch broadly, call Put, extract quotes, advance cursors/history, reconcile
labels, migrate the corpus or write provider state. Whole local source bytes are
limited to 4 MiB per source and 16 MiB across the preview, checked before provider
reads and before loading an oversized body. Oversized sources refuse; nothing is
truncated and presented as full proof. Default stdout is a metadata-only redacted
summary and **cannot be applied**. Only explicit `-out` creates the exact PRIVATE
manifest, exclusively (no overwrite), with mode 0600. That file contains exact
source text/HTML and IDs: treat it as private local mail, not a public fixture.
Apply input is capped at 64 MiB, rejects unknown fields/trailing JSON, and validates
the version/account/items and source budgets before opening any writable database.

Apply performs no provider calls and takes the existing account sync lock. It
revalidates versioned evidence and unchanged source/copy/target/evidence/graph
preconditions transactionally before writing. An exact replay is a no-op. It
writes sidecars/audit only, except for explicitly reviewed source-origin quote
sightings that have independent sightings; their entries and independent/direct
evidence remain. Withdrawn sighting rows are retained in the audit payload.
Unique orphan artifacts are a refusal, **not deletion or a claim of full legacy
cleanup**. Refused manifests do not partially apply. A changed corpus requires a
fresh preview (the conservative graph revision precondition can reject unrelated
concurrent changes too). There is no general rollback UI/command; withdrawn rows
and the exact reviewed manifest remain inspectable in the local apply audit.

## Integration hotspots and remaining verification

- Quote metadata is migration 29; reactions append migration 30. The shared
  frozen reader and independent quote/reaction absorption guards are combined.
- Both apply CLIs use `OpenForMetadata`: unsupported schemas refuse before
  explicit journal-mode/migration setup. Reaction apply also checks schema under
  its account lock and transaction before sidecar writes.
- The exact public dependency is pinned in the main module; no local replacements
  are required. The integrated source still needs its final gates and Nix build.
- All reaction fixtures are synthetic. No provider/credential reads or live
  mailbox verification were performed by these tests.
- Docket token-save compatibility tests inject a pre-write lock failure. They
  do not prove that every reported token-save error precedes activation: a rare
  post-rename directory-fsync failure can report an error after installation.
  Production authentication semantics are unchanged.

Scoped threadless saved pages refresh locally from durable entry IDs only when
there is no recorded query/thread selection. Seeds and their fresh graph closure
are revalidated through the effective account projection; saved bodies and IDs
are not authorization. The existing message budget bounds seed/closure size and
oversized expansions refuse. This fallback does not enable scoped provider fetch
or alter unscoped refresh behavior.
