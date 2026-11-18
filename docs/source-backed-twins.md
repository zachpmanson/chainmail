# Source-backed recovered-mail identity

The ordinary twin clock lattice, token thresholds and `twins_plain` HTML grammar are unchanged. A separate identity witness permits an exact nonempty match to the actual surviving mailbox's own MIME plaintext or original-HTML text (whitespace normalization only) when current source mail proves the identity:

- Re-peel the actual current hosts; require exactly one matching key and recovered author in every relevant claim. Every claim must independently match a supported exact representation of the same actual mailbox target.
- At least one claim must be an actual immediate attribution, whose raw `mail_detail.in_reply_to` names a unique raw `mail_detail.message_id` in a shared Gmail account. Neither inferred graph parents nor References ancestry qualify.
- Original-HTML alternatives reuse `twinHTMLText` and, for known table markup, a block-boundary projection preserving original text whitespace without inserting separators at `td`/`th` boundaries. Address labels are never stripped or rewritten in a claim; case, punctuation, values and visible URLs remain exact. HTML is parsed once per bounded source proof, outside the body cache.
- Nested sightings corroborate the block, author, thread and shared account, but their own immediate parents are not claims about the nested message. Competing immediate claims decline.
- Replay can discover current witnesses among quoted sightings already attached to MIME/HTML-exact mailbox candidates. Discovered-only sightings for other date keys are unrelated; matching-key conflicts cannot be ignored. Current/duplicate-attached sightings remain mandatory. Discovery and re-peeling are bounded and outside the body cache.
- Only a non-MIME exact original-HTML representation can certify that joined cell tokens are not novel prose. The existing changed-destination veto runs first; attachment vetoes remain unchanged. Raw-MIME matches retain their original annotation semantics.
- The existing original-HTML-derived annotation/link veto and dropped-attachment veto still apply. A retained inline annotation keeps its source-proven base even off the clock lattice, with the same derived/parent classification at ingest and repair. The richer mailbox bodies are never rewritten.
- Ingest applies source-backed identity and its sighting in one revision-checked writer. Retained annotations are created and linked in that writer too. Conflicts retry from a fresh read snapshot (at most three attempts); stale source/account/header evidence never authorises an attachment.
- Candidate MIME plus HTML is byte-bounded in SQL before materialisation. Oversized evidence declines rather than removing candidates from ambiguity checks.

Stated source clocks must still fall within the existing coarse 26-hour widest plausible gap; truly undated source claims do not invent a clock. Identity does not prove an offset: `measureOffset` separately checks the unchanged civil-offset lattice. The two off-lattice fixture gaps (5h56m25s and 5h56m12s) produce no offsets. The two mention-expansion copies have an eight-hour gap; only the one with exactly one source host yields an attributed measurement.

Reconciliation rules advance from 6 to 7. Migration 31 invalidates reconciliation on Gmail-copy insertion, deletion and account/entry membership changes, not label/history bookkeeping. Body reduction semantics and cache format are unchanged.

## Invented regression

`internal/mailingest/source_twins_test.go` contains nine invented mailbox messages about greenhouse inventory, plus five distinct recovered entries. All people, addresses, identifiers, prose, dates and inventory quantities are synthetic. Four recoveries duplicate mailbox entries; one original exists only in forward/quote history. The duplicated blocks have 4/3/2/1 source sightings, including nested Gmail plaintext/HTML requotes. The long and short mention replies have the same author and a shared opening. The long reply includes paragraphs and a styled table; the short reply has fewer than 25 prose tokens.

Against the original matcher, recovered copies remain duplicated. With the fix, mailbox-first and interleaved ingest leave 10 immediately; quote-first/reverse ingest first leave 14, dry-run/apply remove four, and repeat removes zero. Nested-first replay leaves 10 without recreating duplicates. Tests retain mailbox bytes, attachments, quoted sightings and acyclic reply edges. A second version adds one later nested host using original HTML mention labels and concatenated table cells: quote-first leaves 15, repair removes four to leave 11; mailbox-first and nested-first replay remain at 11.

Additional corpus tests exercise raw-header/account/author/thread ambiguity, required versus discovered sources, stale edits, nested witness discovery, repeated short replies at distinct keys, exact normalization, data-loss vetoes, cold/warm/reopened plans and account/header/host-mutation ingest writer guards, successful fresh-snapshot retry, truly undated immediate/nested ingest replay, off-lattice HTML-annotation ingest/repair parity, oversized-evidence refusals, mixed exact MIME/HTML claims, changed cell/URL/prose refusals and distant stated generic-reply refusals.

Focused validation:

```sh
nix develop --command bash -c 'export GOMAXPROCS=2; go test -p 2 ./internal/corpus ./internal/mailingest -run "Twin|Requote|Reparse|Reconcile|Forward|Source|Schema" -count=1'
```

The fixtures use only invented data and temporary stores; they do not access a private corpus or live service.
