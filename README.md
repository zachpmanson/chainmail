# Chainmail

Read an email trail as one chronological transcript with its reply structure made
visible. A mail thread hides its own history — a forward can carry months of exchange
that never appear as separate messages — so chainmail **unspools** a trail into flat
entries with provenance and renders it as a navigable, self-contained page:
chronological by absolute time, annotated with who replied to what, its reply chains
drawn as lanes.

Mail and Slack are slurped into one SQLite corpus; pages are found by lexical and
semantic search, curated, and rendered as static HTML (or served by the API).

## Install

```bash
nix develop      # go, node 22, npm
make install     # builds corpus and chainmail-server into ~/.local/bin
npm install      # from the committed lockfile
```

`make help` lists every operating command.

## Setup

Four external pieces; only the first is required — without Slack you have a mail
corpus, without ollama you have lexical search.

1. **Gmail** — mail is read in-process via the app's own **Sign in with Google** bar;
   the next `corpus slurp` reads mail as that account. The bar can connect additional
   accounts; each has its own token and ingest cursor, while identical messages still
   appear once in the unified corpus. Gmail IDs, labels, unread state, and attachment
   fetches stay scoped to their owning account. Mailbox writes infer the account only
   when a selected chain has copies in one account; otherwise pass `accountId` to the
   API request. The inbox account selector filters the unified list to one mailbox;
   `GET /auth/status` lists the stable account IDs and their sign-in state. The
   sign-in banner can disconnect an account; this removes its local token but keeps
   already-imported mail. Disconnecting `legacy` also signs the shared Docket token
   out. Gmail links use an account-neutral URL rather than assuming the browser's
   `/u/0` slot.
2. **Slack** — import a workspace with [slackdump](https://github.com/rusq/slackdump):
   `slackdump workspace import slack.env`, then `slackdump archive -o ~/.local/state/chainmail/slack`.
   Token: devtools console on a logged-in Slack tab (`localStorage.localConfig_v2` →
   your workspace's `token`). Cookie: devtools → Application → Cookies → `https://app.slack.com` →
   the cookie named `d` (HttpOnly). Delete `slack.env` after importing — it is a
   full-read credential. `slackdump resume <dir>` is incremental from then on.
3. **ollama** — needed at **query time**, not just indexing: `OLLAMA_KEEP_ALIVE=-1 ollama serve &`
   and `ollama pull nomic-embed-text` (only if you want `-mode semantic`/`hybrid`).
4. **The corpus**

```bash
export CHAINMAIL_ME=you@example.com        # marks your own messages
corpus init
corpus slurp -since 2026-08-01             # slack, then mail, then settle, then unread, then embed
make doctor                                # what is in it, and what is missing
```

`slurp` is the whole sequence and safe to re-run: ingest is keyed on a content hash
and mail is paged with a resumable cursor. `dedupe` is irreversible: `corpus dedupe -apply`
after `make backup`.

### Incremental Gmail sync

Unbounded `corpus slurp -q in:anywhere` (including the server's full-mail sweep)
uses Gmail History with the in-process Gmail backend. The first run captures a
mailbox history ID, scans every page without a date frontier, then replays changes
that arrived during the scan. Later runs read only IDs with message/label changes,
including old messages archived or moved by Gmail/Docket. Labels are replaced even
when message bodies are unchanged. No additional OAuth grant is requested: the
existing `https://mail.google.com/` scope permits History reads.

Checkpoints and sync locks are per account. An interrupted scan resumes its pages;
an interrupted history replay starts again from the last fully committed ID.
Expired history IDs trigger another complete scan and label reconciliation, not a
date-limited top-up. Permanently deleted messages retain their corpus content but
lose their mailbox labels. History state never authorizes purging stored content.

Custom `-q`, `-since`, `-limit`, and legacy subprocess-backend runs keep the existing
query cursor/frontier path and do not establish history checkpoints. A bounded run
that intervenes in a bootstrap invalidates that pending bootstrap; the next full
sync restarts safely. Each mail run reports list/history pages, full-message read
attempts and elapsed time (the query backend may also fetch envelope metadata).
The first full sync/recovery can be expensive; compare subsequent sweeps for savings.

## Running it

```bash
chainmail-server &      # 127.0.0.1:8765, loopback only — personal mail, no auth
npm run dev             # vite, proxying /v1 to the server
```

Search, tick the chains that belong, build a page from them. Without the server, a spec
on disk still renders: `corpus spec -q "…" -o spec.json && npm run render -- spec.json -o page.html`.

**Saying "now" fetches first.** The nav's ↻ and the saved page's own refresh button both
start with `POST /v1/slurp` — the same ingest the hourly sweep runs — and only then ask the
corpus again (the nav) or re-derive the page (the page's button). Fetch, then read: a
re-read of a corpus the fetch has not written to is the same page twice. Without `-slurp`
the endpoint answers 403 and both controls fall back to re-reading what the corpus already
holds, which is what they were before; a press while a sweep is already running answers
409, since the work is being done.

**Refresh timings** appear as `slurp timing: phase=mail outcome=complete elapsed=12.345s`.
Every selected phase and the final status probe emit a start and an elapsed outcome,
including failures, incomplete walks and skips; the summary retains each duration.
Manual and scheduled sweeps stream these metadata-only lines immediately to
`journalctl -u chainmail -f`, labelled `manual sweep` or `scheduled sweep`, while
preserving the full report returned by the button and logged at the end. A start
without an outcome identifies the active phase if a subprocess is interrupted.
The CLI's `phase=total` measures the ingest; the server's separate `timing: phase=total`
also includes recording the sweep and warming the fold cache before returning.

**Undo:** Ctrl+Z (⌘Z on macOS), or the navigation Undo arrow, reverses read/unread,
archive, trash, move and compact/reply-tree layout choices in session order. Text fields
keep native editing undo. See [Undo](docs/undo.md) for failure handling, lifetime and
operations that cannot safely be reversed (including sends and draft lifecycle changes).

**Unread state is the mailbox's, not the corpus's.** A row carries how many of its
messages Gmail still calls unread, and the reading pane's mark read / mark unread button
changes that in the mailbox itself — so the reader's phone agrees with the page. The
server needs `-mark-read` to be allowed to write (`enableMarkRead` in the nix module);
without it the button says so and changes nothing. The corpus's own copy of the label is
corrected by the `unread` phase of `corpus slurp`, which reads the mailbox's unread set
and fixes every stored label that disagrees with it.

**Timeline** is one chronological column. **Columns** gives one lane per reply chain.
The **reply tree** panel lights the ancestry of the entry you're reading.

The original/simplified HTML default uses server-side structural evidence in the
main authored content; saved reading preferences override it. See
[Original HTML default](ORIGINAL-VIEW.md) for the rules, API contract and known
conservative mismatches.

**Replying starts as reply-all, and who it reaches is the reader's to say.** A thread in the
reading pane ends with a reply box: one row of To and Cc autocomplete fields and a link to
its target message, then the reader's own words. Hovering the link highlights that message.
Pressing *preview* sends
nothing — the server composes the reply and returns it as a plan, and the reader sees the
whole message and its actual recipients before anything leaves. Pressing *send* sends that.
The audience starts from the message being answered — the newest message in the thread Gmail
holds, its sender in `To`, its original `To` and `Cc` in `Cc`. The two compose-screen address
fields let the reader add, remove or move recipients before preview; autocomplete offers the
corpus's people and participants from the message, and an address can also be typed directly.
Untouched lists remain the mailbox's default, so its Reply-To and account-alias handling
stays authoritative. The two things a field refuses are the reader's own address and an
address already on the reply, in either list.

The one thing the reply-all tick decides about the *starting* audience is whether the
message's other people come with it: it is on by default, and clearing it answers the sender
alone. Adding anybody back is the compose fields' business, not the tick's.

**What bounds a send is the grant, not the audience.** `POST /v1/send` is answered only on
the server's loopback bind, with no authentication — a request is whoever can reach the port
— and only when the server was started with `-send-mail` (`enableSendMail` in the nix
module); without it the box says so and nothing is written. A host that leaves that grant on
is a host where whoever can reach the port can name any address they can type and send mail
as the reader to it. A host that must not send to arbitrary addresses is a host that should
not grant it. The addresses in a request are checked for shape — each one has to parse as an
address, and the same address may not be named twice — and for nothing else: whether a domain
exists is the mailbox's answer, not this server's.

The reader's own address is the corpus's answer rather than a setting here: `/v1/settings`
names the person the reader is and `/v1/people` says which addresses that person has, which
is how a field knows what to refuse. A message whose audience is only the reader — a note to
yourself, or two of your own addresses in one thread — comes back with the sender on it,
because a reply to your own message is what such a thread is for; that stays the mailbox's
reply to compose, and the preview shows its authoritative recipient.

**A reply goes out in both forms, and the reader can send the text alone.** The message is
one `multipart/alternative`: the plain text it has always been, and the same words as HTML
beside it — the reader's paragraphs, and the message being answered inside a `blockquote` so
a client that renders markup does not read the quote as part of the answer. What sits inside
that blockquote is the answered message's own markup when it had any — its lists, its links,
its tables, and the quote it was itself carrying — and paragraphs of its text when it did not,
so an answer to an HTML mail carries the mail rather than a transcript of it. A message that
came as text is quoted as text in both parts, its own `>` markers and all: a text part has
nowhere to put markup, and the two readings of one message are the message's own.

A reply's markup is therefore not all the server's: the quote is the answered message's own,
put through the same allowlist every body in the reading pane passes, so an answer can relay
to its recipients nothing the page would refuse to render. The reader's own words, the
heading, and every word of a message that came as text are escaped as they always were. Both
parts come from one call (`spec.ComposeReply`), from the same words and the same message, each
quoting it in the form that message was sent in; nothing is converted from the other. The
preview shows the text part, which is the one that says what the message says.

**Gmail drafts sync across clients.** Chainmail autosaves compose and reply text to Gmail,
shows the account's saved drafts in the new-message composer, and automatically resumes a
reply draft when its Gmail thread is opened. Gmail remains the source of truth; Chainmail
keeps no draft database. Saving/updating/deleting drafts requires `-draft-mail`
(`enableDraftMail` in the Nix module), off by default. Sending a saved draft still requires
`-send-mail`. Both grants matter because this server is loopback-only with no authentication:
anyone who can reach it can use an enabled draft-write endpoint. Draft send uses Gmail's
saved content; confirmed sends are filed back into the corpus when possible. The existing
OAuth grant already requests `https://mail.google.com/`, which includes draft operations, so
this feature does not require a narrower-scope re-consent.

An **`html`** field turns the second part off for one reply (absent means it goes, so an
older client sends the message it always sent). A reader who knows their correspondent or a
list wants the words alone can say so, and what they get is byte for byte the text part they
would have had anyway — the HTML is a second rendering of that, never the source of it.
The server needs `-send-mail` to do it (`enableSendMail` in the nix module); without it
the box says so and nothing is written. A reply is filed into the corpus in the same
request, so the answer appears in the trail immediately rather than at the next slurp —
and if that filing fails the reply has still been sent, which the log records rather than
the reply falsely failing.

## Re-running

Every rendered page embeds the spec that produced it, so a later pass reloads
structured input instead of scraping HTML:

```bash
corpus refresh page.html -o new.json       # what has arrived since
npm run render -- new.json -o page.html --since page.prev.html
```

`--since` reports what is **new** and what is **revised**; `corpus refresh` restores
every recorded chain whole and re-runs the recorded queries, whose new finds are
printed as **proposals** rather than included — a curated page cannot re-widen on its
own. `make repage P=page.html` runs the loop.

## The contract

Input is a **timeline spec**: JSON conforming to `schema/timeline.schema.json`. A
collector produces it; chainmail only renders. The two load-bearing fields are
`parent` (what each entry replies to — drives ordering, lanes, the reply tree) and
`tz` (ordering is by *absolute* time; a missing zone is inferred from what that sender
stated elsewhere and never allowed to invert a reply chain).

## Development

```bash
nix develop             # go, node 22, npm, typescript-language-server
npm ci                  # install the locked frontend dependencies
make check              # tests, ESLint, Prettier check, go vet/gofmt, typecheck
npm run format          # format TypeScript/TSX
npm run format:check    # check formatting without changing files
# CSS is excluded: source-level tests assert exact CSS declarations.
npm run gen:types  # regenerate src/lib/timeline/spec.d.ts from the schema
npm run gen:api    # regenerate src/lib/api/api.d.ts from openapi.json
```

The Go side has no direct dependencies; the JS runtime is three packages (`react`,
`@tanstack/react-query`, `openapi-fetch`) — keep it that way unless a dependency earns
itself. Generated files are never hand-edited. `fixtures/synthetic.json` is a
full-complexity trail for the renderer; real trails are sensitive and stay untracked at
`fixtures/local.json` (drop it on the page). `corpus eval` A/Bs retrieval configs over
a judged query set (`fixtures/eval.synthetic.json`).