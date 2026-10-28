package corpus

// migrations are applied in order; each is recorded in schema_version so a
// re-open is a no-op. Never edit an applied migration — append a new one.
var migrations = []string{
	// 1: the core store.
	//
	// Identity notes, since they were the contested part of the design:
	//
	//   * `id` is an integer primary key (a rowid alias) because FTS5
	//     external-content tables and sqlite-vec both join on rowid, and a text
	//     primary key would be a second B-tree with the real rowid left hidden
	//     and unnamed.
	//   * `ext_id` is the natural key and the only durable identity — neither
	//     rowids nor generated ids survive a rebuild. It is what any outward
	//     reference should use.
	//   * `parent_id` carries a foreign key because it is only ever set once
	//     resolved. `parent_ref` holds the raw Message-ID / thread_ts and is
	//     deliberately unconstrained: a parent outside the mailbox is the normal
	//     case, not corruption, and losing it would discard evidence that a
	//     conversation continues somewhere unseen.
	`
	create table people (
	  id           integer primary key,
	  display_name text not null
	);

	create table identities (
	  person_id integer not null references people(id),
	  kind      text not null,          -- email | slack_uid | display_name
	  value     text not null,
	  rule      text,                   -- which rule matched, so a bad merge is traceable
	  primary key (kind, value)
	);

	create table entries (
	  id          integer primary key,
	  source      text not null,        -- mail | slack
	  ext_id      text not null,        -- 'mail:<message-id>' | 'slack:<ch>:<ts>' | 'quote:<sha>'
	  kind        text not null default 'message',   -- message | note
	  ts          integer not null,     -- epoch UTC; the only cross-source sort key
	  tz          text,                 -- as stated by the source, for display
	  person_id   integer references people(id),
	  container   text,                 -- mail thread id | slack channel id
	  parent_id   integer references entries(id),
	  parent_ref  text,                 -- raw Message-ID / thread_ts, kept after resolution
	  subject     text,
	  body_html   text,
	  body_text   text,
	  permalink   text,
	  body_sha    text not null,        -- idempotent re-ingest; a change means "revised"
	  ingested_at integer not null,
	  unique(source, ext_id)
	);

	create index entries_ts        on entries(ts);
	create index entries_container on entries(container, ts);
	create index entries_parent    on entries(parent_id);
	create index entries_person    on entries(person_id, ts);
	create index entries_unres     on entries(parent_ref) where parent_id is null;

	create table mail_detail (
	  entry_id    integer primary key references entries(id),
	  gmail_id    text,
	  message_id  text,
	  in_reply_to text,
	  refs        text,                 -- References, space-joined as received
	  from_addr   text,
	  to_addr     text,
	  cc_addr     text,
	  labels      text
	);
	create index mail_message_id on mail_detail(message_id);

	create table slack_detail (
	  entry_id    integer primary key references entries(id),
	  channel_id  text,
	  ts          text,
	  thread_ts   text,
	  reply_count integer,
	  subtype     text,
	  is_bot      integer not null default 0,
	  is_dm       integer not null default 0
	);

	-- One row per place an entry was seen. The same original is quoted across
	-- many forwards, so provenance is many-to-one and cannot be a single column.
	create table sightings (
	  entry_id integer not null references entries(id),
	  seen_in  integer references entries(id),   -- the message we found it inside
	  kind     text not null,                    -- direct | quoted | forwarded
	  detail   text,
	  primary key (entry_id, seen_in, kind)
	);

	create table attachments (
	  entry_id   integer not null references entries(id),
	  name       text not null,
	  mime       text,
	  size       integer,
	  permalink  text,
	  source_ref text                            -- gmail part id | slack file id
	);
	create index attachments_entry on attachments(entry_id);
	`,

	// 2: search indexes. External content, so bodies are not stored twice.
	// Two tokenizers on purpose: porter for prose, trigram for identifiers like
	// CINV-00066864, which the word tokenizers split badly.
	`
	create virtual table entries_fts using fts5(
	  subject, body_text,
	  content='entries', content_rowid='id',
	  tokenize='porter unicode61'
	);

	create virtual table entries_ident using fts5(
	  body_text,
	  content='entries', content_rowid='id',
	  tokenize='trigram'
	);
	`,

	// 3: participation, and an audit trail for merges.
	//
	// entries.person_id names the author only, so anyone who never sent anything
	// was invisible: on a real 58-entry trail, 4 of 15 participants appear solely
	// in To:/Cc:. "Routed to four cc'd people" is a different fact from "sent to
	// nobody", so recipients get rows of their own. The author is recorded here
	// too, as role='from', so one query answers "who was involved" without
	// unioning two shapes; entries.person_id stays as the authoritative author.
	//
	// person_merges keeps merges traceable. identities.rule records how an
	// identity was first matched and is deliberately left intact by a merge — the
	// merge itself is the thing that needs its own record, since it is the step a
	// human can get wrong.
	`
	create table participants (
	  entry_id  integer not null references entries(id),
	  person_id integer not null references people(id),
	  role      text not null,          -- from | to | cc
	  primary key (entry_id, person_id, role)
	);
	create index participants_person on participants(person_id, role);

	create table person_merges (
	  kept_id      integer not null references people(id),
	  dropped_id   integer not null,    -- the row is gone, so no foreign key
	  dropped_name text,
	  reason       text,
	  merged_at    integer not null
	);
	`,

	// 4: store the UTC offset, not just the zone label.
	//
	// `tz` holds what the source stated ("NZST", "+0530"). Rendering the sender's
	// own clock needs the offset in minutes, and a label alone does not give it:
	// spec generation had to mirror a label->offset table from the renderer, and
	// an unrecognised label left no honest option but a UTC clock. The Date header
	// carries the offset directly, so capture it at ingest and the whole class of
	// caveat disappears.
	//
	// `org` likewise: colour and grouping are driven by org, and people held only
	// a display name, so it was being inferred from the mail domain. An inferred
	// value is fine as a default but should be storable and correctable.
	`
	alter table entries add column tz_offset integer;   -- minutes east of UTC
	alter table people  add column org text;
	`,

	// 5: domain aliases, so a rebrand does not split people in two.
	//
	// Accounts predating a rename keep the old domain while later ones use the
	// new: the same human is zach@old and zach@new, and address-keyed resolution
	// creates two people and splits their entries. This is data rather than code
	// because the mapping is a fact about an organisation, not about the parser,
	// and because adding one must be able to repair the rows already ingested.
	`
	create table domain_aliases (
	  from_domain text primary key,
	  to_domain   text not null,
	  note        text,
	  added_at    integer not null
	);
	`,

	// 6: mark entries recovered from quoted text.
	//
	// Derivable from the 'quote:' ext_id prefix, but only by a LIKE scan, and this
	// flag is in the hot path of every search that wants to weight real messages
	// above recovered ones. A quoted block whose Message-ID matches a real message
	// merges into that entry and stays quoted=0: it is the same message, and the
	// mailbox copy is the better one.
	`
	alter table entries add column quoted integer not null default 0;
	create index entries_quoted on entries(quoted, ts);
	`,

	// 7: name the Slack channel an entry came from.
	//
	// entries.container holds the channel id, which is the only thing stable
	// enough to key on — a channel can be renamed and #general is not unique
	// across workspaces. But an id is unreadable, and nothing else in the corpus
	// carries the name, so a rendered Slack timeline could only ever say
	// "C042NF1TTK8". The name is a display label, denormalised per entry on
	// purpose: it records what the channel was called when the message was
	// archived, which is what a reader of that message wants.
	//
	// Deliberately NOT in entries.subject, where it would be indexed at the
	// subject column's weight on every message in the channel — searching for
	// "general" would then rank the whole of #general above an actual mention.
	`
	alter table slack_detail add column channel_name text;
	create index slack_channel on slack_detail(channel_id, ts);
	`,

	// 8: one vector per entry per model, for semantic retrieval.
	//
	// A vector is meaningless without knowing what produced it: two models, or
	// two dimensions of one model, in one column returns confident nonsense and
	// nothing in the numbers reveals it. So `model` and `dim` are stored beside
	// every row and every read filters on both.
	//
	// `model` is in the primary key rather than a plain column so a model change
	// is a background migration instead of an outage: the old vectors stay
	// searchable while the new ones accumulate, and the switch happens when the
	// backfill finishes. With entry_id alone as the key, re-embedding in place
	// would leave search finding only the part of the corpus already converted,
	// which looks like a corpus that lost half its mail. `corpus embed -prune`
	// reclaims the superseded rows afterwards.
	//
	// Staleness is `body_sha` plus `prep`. body_sha already covers subject and
	// body text, which is exactly the input to the embedded string — but only
	// given a fixed way of deriving that string from them, and the derivation
	// drops quoted history, so an improvement to it changes every vector without
	// changing a single body_sha. `prep` is that derivation's version, and
	// bumping it re-embeds the corpus. The alternative, hashing the prepared
	// text itself, needs no constant to remember but cannot answer "what is
	// stale" in SQL: it would peel 29k bodies on every run just to find out
	// there was nothing to do.
	//
	// `vec` is NULL for an entry deliberately not embedded, with `skip` saying
	// why. A row is still written, because otherwise every run reconsiders the
	// same third of the corpus that has nothing worth embedding, and a
	// zero-content entry is a fact about the mail worth being able to count.
	//
	// Vectors are stored L2-normalised, little-endian float32: 4 bytes a
	// dimension, so similarity is a dot product and a query costs one pass. No
	// vector index — modernc.org/sqlite cannot load native extensions, so
	// sqlite-vec is unavailable, and a brute-force scan at this corpus's size is
	// milliseconds. An index would start to matter around a million entries.
	`
	create table entry_embeddings (
	  entry_id    integer not null references entries(id),
	  model       text not null,
	  dim         integer not null,
	  body_sha    text not null,        -- as of embedding; a change means stale
	  prep        integer not null,     -- text-preparation version
	  vec         blob,                 -- NULL when skipped
	  skip        text,                 -- why it was not embedded
	  embedded_at integer not null,
	  primary key (entry_id, model)
	);
	create index entry_embeddings_model on entry_embeddings(model, dim)
	  where vec is not null;
	`,

	// 9: the offsets a quoting client was measured at.
	//
	// A message stored both in the mailbox and as a quote of itself states its own
	// send time twice: once as the true instant, once as the wall clock the
	// quoting client rendered. The difference is that client's UTC offset exactly,
	// which is otherwise the quantity tzinfer has to infer from where the quoter's
	// own mail says they are — and it is the only evidence that reaches an
	// Exchange sender stamping +0000 on its own mail while rendering everyone
	// else's at +1200.
	//
	// Stored rather than derived, unlike Places(): collapsing the twin deletes the
	// quoted row, and that row is the only place the wall clock ever existed. A
	// measurement nobody wrote down at that moment cannot be recovered afterwards.
	//
	// Keyed on (person, entry, off) so re-running the pass is a no-op while two
	// contradictory offsets from one host both survive — a client rendering two
	// clocks at two offsets in one message is a fact about the client, not a row
	// to overwrite. `measured_from` names the quoted entry the measurement came
	// from and carries no foreign key, for the same reason person_merges.dropped_id
	// does not: the row it names is gone.
	`
	create table render_offsets (
	  person_id     integer not null references people(id),
	  entry_id      integer not null references entries(id),  -- rendered in this message
	  at            integer not null,   -- when it rendered, so a zone fit reads the right side of DST
	  off           integer not null,   -- minutes east of UTC
	  measured_from integer not null,
	  measured_at   integer not null,
	  primary key (person_id, entry_id, off)
	);
	create index render_offsets_person on render_offsets(person_id, at);
	`,

	// 10: where a walk of a source got to, so a killed ingest resumes.
	//
	// Keyed on (source, container). A mail container is the query string itself,
	// not a label: two different queries cover two different sets and a cursor
	// that conflated them would claim coverage it never had. Slack keys on the
	// channel id for the same reason its entries do.
	//
	// `position` is opaque and source-defined — for mail it is docket's page
	// token. It is only meaningful while `complete` is 0: a walk that finished
	// has no position left to hold, and a stale token from a finished walk would
	// resume in the middle of a set already covered.
	//
	// `frontier` is the newest entry timestamp a COMPLETED walk covered, and is
	// the whole reason a top-up is cheap: paging runs newest-first, so a walk can
	// stop once it reaches ground below the frontier. Written only on completion,
	// because a frontier advanced by a partial walk would certify coverage of a
	// gap the kill left behind.
	//
	// `succeeded_at` is the last completion, kept separately from `updated_at`
	// so "we have written to this cursor recently" cannot be mistaken for "this
	// container is fully covered".
	`
	create table cursors (
	  source       text not null,       -- mail | slack
	  container    text not null,       -- mail: the query | slack: channel id
	  position     text,                -- resume point, source-defined; set while incomplete
	  frontier     integer,             -- newest ts a completed walk covered
	  complete     integer not null default 0,
	  walked       integer not null,    -- entries walked since the last completion
	  updated_at   integer not null,
	  succeeded_at integer,
	  primary key (source, container)
	);
	`,

	// 11: a modified copy of a quoted message — the DERIVED classification from
	// the graded overlap in twins.go/FindDerived. Persisted because deciding
	// (vs a twin, vs unrelated) needs the whole corpus and belongs at ingest,
	// not re-derived at render. The renderer reads it to draw the quoter's edit
	// inline inside the message that quoted it instead of a floating duplicate.
	`alter table entries add column derived integer not null default 0;`,

	// 12: attachment bytes, filed once and pulled on purpose.
	//
	// Until now the corpus held attachment METADATA only — a mail attachment was
	// a chip that sent the reader to Gmail, and only a Slack attachment could
	// ever show a picture, because slackdump happened to leave its bytes beside
	// the archive. Storing the bytes is what makes a rendered page genuinely
	// self-contained and stops a preview depending on a directory outside the
	// database — which is also what a headless host's cron slurp cannot promise.
	//
	// Content-addressed rather than a `data` column on attachments, for two
	// reasons. The same screenshot forwarded five times is one row, and docket
	// already hands back the digest, so the key is free. And `Put` replaces an
	// entry's attachment rows WHOLESALE on every re-slurp: bytes hung off one of
	// those rows would be silently dropped by the next hourly walk. blob_sha is
	// re-linked by (entry, source_ref) across that replacement instead, so a blob
	// outlives the row that pointed at it.
	//
	// `media_skip` records a part that was deliberately not pulled or could not
	// be, for the same reason entry_embeddings.skip exists: without it every pass
	// re-requests the same 25 MB video, and "we decided not to" is
	// indistinguishable from "we have not got to it yet". It is a fact worth
	// being able to count, not an error.
	//
	// Nothing here is filled by `slurp`. A mail part costs a Gmail round trip, so
	// pulling is explicit — per message, per thread, per page — and a corpus
	// nobody asked has no media in it.
	`
	create table blobs (
	  sha256     text primary key,   -- over the decoded bytes
	  bytes      blob not null,
	  mime       text,
	  size       integer not null,
	  source     text not null,      -- mail | slack
	  fetched_at integer not null
	);

	alter table attachments add column blob_sha text references blobs(sha256);
	alter table attachments add column media_skip text;
	create index attachments_blob on attachments(entry_id) where blob_sha is not null;
	`,

	// 13: settings — the few choices that are about the reader rather than about
	// the mail.
	//
	// A key/value table because the alternative is a column per preference, and a
	// preference is not evidence: nothing here describes a message, so nothing
	// here may influence what the corpus says. It is stored server-side rather
	// than in the browser because the reader has more than one browser — the
	// point of "open in this folder" is that it is true on the phone as well.
	//
	// A missing row and an empty value mean the same thing, a setting nobody has
	// made: there is no third state for "unset" to be confused with.
	`
	create table settings (
	  key   text primary key,
	  value text not null
	);
	`,

	// 14: org_domains — which mail domains are one organisation.
	//
	// A message's colour is its sender's organisation, and an organisation is
	// guessed from the sender's mail domain: the label left of the public suffix,
	// skipping freemail. That guess is right until a company mails from two
	// domains, or a domain's guess reads badly, and this table is where the reader
	// overrides it. Two rows naming the same org *are* one organisation — grouping
	// domains is nothing more than giving them one name — and a row with an empty
	// org says the domain is not an organisation at all, which is a different
	// claim from having no row.
	//
	// Deliberately not `domain_aliases`. That table merges identities and decides
	// who people are; this one decides only what colour their mail is drawn in.
	// Named differently, stored separately, because the two are worth confusing
	// exactly once.
	`
	create table org_domains (
	  domain text primary key,
	  org    text not null default ''
	);
	`,

	// 15: prefer_original — whether a sender's mail is read as the sender wrote it.
	//
	// A choice about a person rather than about a browser, which is what moved it
	// out of this reader's localStorage and onto the corpus: the same reader has a
	// desktop and a phone, and "read Ada's mail as Ada wrote it" is true on both or
	// it is not true. It is stated as a person for the reason settings states
	// mePersonId as one: which addresses are one human is the identity graph's
	// answer, and a second copy of that answer here would go stale the first time
	// an alias was learned or two people were merged. A merge moves the answer with
	// the person it is about, which is the whole point of keying it this way.
	//
	// A column on people rather than a key/value row in settings, because this is
	// not a reader-level preference: settings holds what the reader wants of the
	// app, and this is what the reader has decided about one of their
	// correspondents.
	`
	alter table people add column prefer_original integer not null default 0;
	`,

	// 16: mailbox_labels — the folder list the mailbox itself defines, not the
	// labels mail happened to arrive under.
	//
	// mail_detail.labels can only name a label some stored message carries, so a
	// folder created a minute ago with nothing filed under it is invisible, and
	// pressing refresh cannot help: the ingest re-reads a corpus, and the corpus
	// never learned about it. This table is where the ingest records the mailbox's
	// own list (Gmail's Users.Labels.List) so the folder list can be the mailbox's
	// rather than a count of what was ingested.
	//
	// A table rather than a settings row: a label name is arbitrary text and may
	// contain a comma, so there is no delimiter that would not need escaping. One
	// row per label is what the thing is, and the counts beside it stay the
	// corpus's own — see Store.Labels.
	//
	// An empty table means the ingest has never learned the list, which is the
	// same answer as a host with no mailbox: the folder list falls back to the
	// corpus-derived one rather than serving an empty list. Nothing here is
	// deleted on a failed read either, so a temporary mailbox outage does not
	// blank the folder list.
	`
	create table mailbox_labels (
	  name text primary key
	);
	`,

	// 17: Gmail accounts and account-local copies of logical mail entries.
	//
	// Gmail message ids are local to a mailbox, while Chainmail entries are
	// logical messages shared by the unified corpus. Keeping mailbox copies in a
	// separate relation lets one logical message belong to several accounts and
	// keeps labels account-specific. The legacy account preserves the existing
	// single-token installation during migration; its email is intentionally
	// unknown until the account is identified during the auth migration.
	`
	create table gmail_accounts (
	  id           text primary key,
	  email        text unique,
	  display_name text not null,
	  created_at   integer not null
	);

	insert into gmail_accounts(id, email, display_name, created_at)
	values ('legacy', null, 'Legacy Gmail account', unixepoch());

	create table gmail_copies (
	  account_id text not null references gmail_accounts(id),
	  gmail_id   text not null,
	  entry_id   integer not null references entries(id),
	  labels     text not null default '',
	  primary key (account_id, gmail_id),
	  unique (account_id, entry_id)
	);
	create index gmail_copies_entry on gmail_copies(entry_id);

	insert into gmail_copies(account_id, gmail_id, entry_id, labels)
	select 'legacy', md.gmail_id, md.entry_id, coalesce(md.labels, '')
	from mail_detail md
	where md.gmail_id is not null and md.gmail_id != '';
	`,

	// 18: mail cursors scoped to a connected Gmail account.
	//
	// Gmail page tokens and coverage frontiers are mailbox-local. The original
	// cursor key (source, query) could let one account's progress certify another
	// account's coverage, so mail moves to its own table keyed by account and
	// query. Existing mail cursors belong to the migrated legacy account.
	`
	create table gmail_cursors (
	  account_id   text not null references gmail_accounts(id),
	  query        text not null,
	  position     text,
	  frontier     integer,
	  complete     integer not null default 0,
	  walked       integer not null,
	  updated_at   integer not null,
	  succeeded_at integer,
	  primary key (account_id, query)
	);

	insert into gmail_cursors(account_id, query, position, frontier, complete,
	                          walked, updated_at, succeeded_at)
	select 'legacy', container, position, frontier, complete, walked, updated_at, succeeded_at
	from cursors where source='mail';
	delete from cursors where source='mail';
	`,

	// 19: mailbox label lists are account-local, just like the labels on copies.
	// Preserve the existing list as the legacy account's snapshot.
	`
	create table gmail_account_labels (
	  account_id text not null references gmail_accounts(id),
	  name       text not null,
	  primary key (account_id, name)
	);
	insert into gmail_account_labels(account_id, name)
	select 'legacy', name from mailbox_labels;
	`,

	// 20: distinguish an unset sender style from an explicit plain-text choice.
	`
	alter table people add column prefer_original_set integer not null default 0;
	`,
	// 21: a fetched mailbox copy is authoritative, even if its quote arrived first.
	`
	update entries set quoted=0 where quoted=1 and (
	  exists (select 1 from gmail_copies gc where gc.entry_id=entries.id)
	  or exists (select 1 from mail_detail md where md.entry_id=entries.id
	             and coalesce(md.gmail_id,'') != '')
	);
	`,

	// 22: NULL-host (direct) sightings must be idempotent too. Retain the latest
	// detail, matching Sight's update semantics for non-NULL hosts.
	`
	delete from sightings where seen_in is null and rowid not in (
	  select max(rowid) from sightings where seen_in is null group by entry_id, kind
	);
	create unique index sightings_direct on sightings(entry_id, kind) where seen_in is null;
	`,

	// 23: mailbox-wide history checkpoints, separate from query cursors. A
	// bootstrap ID is captured before scanning and survives interrupted scans.
	`
	create table gmail_history (
	  account_id text primary key references gmail_accounts(id),
	  history_id text not null default '',
	  bootstrap_id text not null default ''
	);
	alter table gmail_copies add column seen_history_id text not null default '';
	`,

	// 24: durable normalized suffix contributions and precise source invalidation.
	`
 create table fold_state (
 id integer primary key check(id=1), ready integer not null default 0,
 exact_version integer not null default 0, direct_version integer not null default 0,
 normalize_version integer not null default 0, fingerprint_version integer not null default 0,
 window integer not null default 0, generation integer not null default 0,
 aliases_dirty integer not null default 0);
 insert into fold_state(id) values(1);
 create table fold_dirty_entries(entry_id integer primary key);
 create table fold_dirty_people(person_id integer primary key);
 -- No source FKs: deleted sources must retain their published subtraction data.
 create table fold_members(entry_id integer primary key, exact_digest blob not null,
 direct integer not null, person_id integer not null, raw_domain text not null,
 domain text not null, ok integer not null, line_count integer not null);
 create index fold_members_person on fold_members(person_id);
 create index fold_members_raw_domain on fold_members(raw_domain);
 create table fold_tails(entry_id integer not null references fold_members(entry_id) on delete cascade,
 n integer not null, fp blob not null, primary key(entry_id,n));
 create table fold_counts(kind integer not null, group_key text not null, fp blob not null,
 count integer not null check(count>0), senders integer not null check(senders>0),
 primary key(kind,group_key,fp));
 create table fold_sender_refs(domain text not null, fp blob not null, person_id integer not null,
 refs integer not null check(refs>0), primary key(domain,fp,person_id));
 create index identities_person_kind on identities(person_id,kind);
 create trigger fold_entries_insert after insert on entries when new.source='mail' begin
 insert into fold_dirty_entries values(new.id) on conflict do nothing; end;
 create trigger fold_entries_delete after delete on entries when old.source='mail' begin
 insert into fold_dirty_entries values(old.id) on conflict do nothing; end;
 create trigger fold_entries_update after update of source,body_text,person_id on entries
 when (old.source='mail' or new.source='mail') and
 (old.source is not new.source or old.body_text is not new.body_text or old.person_id is not new.person_id) begin
 insert into fold_dirty_entries values(old.id) on conflict do nothing;
 insert into fold_dirty_entries values(new.id) on conflict do nothing; end;
create trigger fold_mail_detail_insert after insert on mail_detail  begin insert into fold_dirty_entries values(new.entry_id) on conflict do nothing; end;
create trigger fold_mail_detail_delete after delete on mail_detail  begin insert into fold_dirty_entries values(old.entry_id) on conflict do nothing; end;
create trigger fold_mail_detail_update after update of entry_id,from_addr on mail_detail when old.entry_id is not new.entry_id or old.from_addr is not new.from_addr begin insert into fold_dirty_entries values(old.entry_id) on conflict do nothing;
insert into fold_dirty_entries values(new.entry_id) on conflict do nothing; end;
create trigger fold_identities_insert after insert on identities  begin insert into fold_dirty_people select new.person_id where new.kind='email' on conflict do nothing; end;
create trigger fold_identities_delete after delete on identities  begin insert into fold_dirty_people select old.person_id where old.kind='email' on conflict do nothing; end;
create trigger fold_identities_update after update of person_id,kind,value on identities when old.person_id is not new.person_id or old.kind is not new.kind or old.value is not new.value begin insert into fold_dirty_people select old.person_id where old.kind='email' on conflict do nothing;
insert into fold_dirty_people select new.person_id where new.kind='email' on conflict do nothing; end;
create trigger fold_domain_aliases_insert after insert on domain_aliases  begin update fold_state set aliases_dirty=1 where id=1; end;
create trigger fold_domain_aliases_delete after delete on domain_aliases  begin update fold_state set aliases_dirty=1 where id=1; end;
create trigger fold_domain_aliases_update after update of from_domain,to_domain on domain_aliases when old.from_domain is not new.from_domain or old.to_domain is not new.to_domain begin update fold_state set aliases_dirty=1 where id=1; end;
create trigger fold_sightings_insert after insert on sightings
 when new.kind='direct' and exists(select 1 from entries where id=new.entry_id and source='mail')
 and (select count(*) from sightings where entry_id=new.entry_id and kind='direct')=1 begin
 insert into fold_dirty_entries values(new.entry_id) on conflict do nothing; end;
 create trigger fold_sightings_delete after delete on sightings
 when old.kind='direct' and exists(select 1 from entries where id=old.entry_id and source='mail')
 and not exists(select 1 from sightings where entry_id=old.entry_id and kind='direct') begin
 insert into fold_dirty_entries values(old.entry_id) on conflict do nothing; end;
 create trigger fold_sightings_update after update of entry_id,kind on sightings
 when old.entry_id is not new.entry_id or old.kind is not new.kind begin
 insert into fold_dirty_entries select old.entry_id where old.kind='direct'
 and exists(select 1 from entries where id=old.entry_id and source='mail')
 and not exists(select 1 from sightings where entry_id=old.entry_id and kind='direct') on conflict do nothing;
 insert into fold_dirty_entries select new.entry_id where new.kind='direct'
 and exists(select 1 from entries where id=new.entry_id and source='mail')
 and (select count(*) from sightings where entry_id=new.entry_id and kind='direct')=1 on conflict do nothing;
 end;`,
}
