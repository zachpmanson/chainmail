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
   the next `corpus slurp` reads mail as that account.
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

**Unread state is the mailbox's, not the corpus's.** A row carries how many of its
messages Gmail still calls unread, and the reading pane's mark read / mark unread button
changes that in the mailbox itself — so the reader's phone agrees with the page. The
server needs `-mark-read` to be allowed to write (`enableMarkRead` in the nix module);
without it the button says so and changes nothing. The corpus's own copy of the label is
corrected by the `unread` phase of `corpus slurp`, which reads the mailbox's unread set
and fixes every stored label that disagrees with it.

**Timeline** is one chronological column. **Columns** gives one lane per reply chain.
The **reply tree** panel lights the ancestry of the entry you're reading.

**Replying is reply-all, and off by default, and who it reaches is the reader's to
say.** A thread in the reading pane ends with a reply box: one field, the reader's own
words, and the message being answered quoted under them by the server. Pressing *preview*
sends nothing — the server composes the reply and returns it as a plan, and the reader sees
the whole message, recipients and quote and all, before anything leaves. Pressing *send*
sends that. The plan's audience starts from the message being answered — the newest message
in the thread Gmail holds, its sender in `To`, its original `To` and `Cc` in `Cc` — and is
edited in the two **address fields** the preview draws, one for each list. Every address is
a chip that can be taken off the reply or moved to the other list, and either field takes an
address typed into it: what is offered is the corpus's own people plus the audience of the
message being answered, and a reader may equally type an address no corpus has ever seen.
So a reply reaches whoever the reader names it for. The two things a field refuses are the
reader's own address and an address that is already on the reply, in either list.

The one thing the reply-all tick decides about the *starting* audience is whether the
message's other people come with it: it is on by default, and clearing it answers the sender
alone. Adding anybody back is the fields' business, not the tick's.

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
reply to compose, and the field shows it like any other address.

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
nix develop        # go, node 22, npm, typescript-language-server
make check         # go test + vet + gofmt, vitest, typecheck — everything
npm run gen:types  # regenerate src/lib/spec.d.ts from the schema
npm run gen:api    # regenerate src/lib/api.d.ts from openapi.json
```

The Go side has no direct dependencies; the JS runtime is three packages (`react`,
`@tanstack/react-query`, `openapi-fetch`) — keep it that way unless a dependency earns
itself. Generated files are never hand-edited. `fixtures/synthetic.json` is a
full-complexity trail for the renderer; real trails are sensitive and stay untracked at
`fixtures/local.json` (loaded with `?spec=`). `corpus eval` A/Bs retrieval configs over
a judged query set (`fixtures/eval.synthetic.json`).