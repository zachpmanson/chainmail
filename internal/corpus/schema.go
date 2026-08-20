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
}
