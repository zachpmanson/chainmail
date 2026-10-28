# SQLite writer contention

Source ingestion and render-driven fold publication can run in different
processes against one WAL database. SQLite serializes their writers; no
process-local lock can replace that coordination.

`corpus.Open` configures **every physical pooled connection** through the DSN:
foreign keys enabled, a **30,000 ms busy timeout**, and `_txlock=immediate`.
Ordinary `Begin` callers are producers: they reserve writer ownership **before
reading**, avoiding a deferred read snapshot that later fails to upgrade with
`SQLITE_BUSY_SNAPSHOT` (517). Retrying a statement in that obsolete transaction
cannot fix it. We never retry transaction bodies or multi-statement prefixes.

The 30-second per-acquisition budget accommodates the observed ~10-second first
fold publication, with headroom. It is not a guarantee that all contention
succeeds: longer writers can still cause `SQLITE_BUSY`, and contextless producer
APIs can block for that budget. It is not a whole-operation deadline either.

SQLite's busy-handler sleep is not promptly interrupted by context cancellation.
Refresh and embedding therefore pin a connection, save its actual busy timeout,
and temporarily set it to zero. They retry only `BEGIN IMMEDIATE`, checking the
context between attempts. Refresh retains its five-second acquisition budget;
embedding uses 30 seconds. Statements and commit use the caller's context, with
an explicit cancellation check before commit. Inference happens before acquiring
the embedding writer, so model latency never holds the database writer lock.

Before returning a pinned connection to the pool, cleanup rolls back any active
transaction using an uncancelled context, then restores the saved timeout. A
rollback or restoration failure discards the physical connection instead of
pooling uncertain transaction state or a zero timeout. Cleanup failures are
returned to the caller.

`WithFoldSnapshot` explicitly uses `ReadOnly: true`, which bypasses the driver's
configured immediate begin mode. It keeps an old coherent WAL snapshot while an
external writer commits. **The option does not enforce read-only SQL:** callbacks
must not write, commit, or retain that transaction.

Migration 24's durable dirty queues, SQL invalidation triggers and atomic fold
publication are unchanged. These policies do not require a corpus rebuild or
schema rollback. This code change does not authorize a production deployment.

Regression coverage in `internal/corpus/writer_test.go` gates actual fold
publication using a test-only SQLite function and connection-local trigger, then
runs actual mail, Slack, unread, quoted-enrichment and embedding producers. It
also demonstrates stale snapshot 517, deadline cancellation without late writes,
in-flight embedding rollback, per-connection policy and restoration/reuse. The
existing fold concurrency, trigger and restart tests cover coherent snapshots,
raw writers, mixed versions and process death during publication.
