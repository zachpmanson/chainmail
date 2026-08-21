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
}
