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

**Replying is reply-all, and off by default.** A thread in the reading pane ends with a
reply box: one field, the reader's own words, and the message being answered quoted under
them by the server. Pressing *preview* sends nothing — the server composes the reply and
returns it as a plan, and the reader sees the whole message, recipients and quote and all,
before anything leaves. Pressing *send* sends that. There is deliberately no recipient
field: the box answers the newest message in the thread Gmail holds, and the recipients are
that message's own — its sender in `To`, and its original `To` and `Cc` in `Cc` — minus
every address the mailbox itself owns, so a loopback server behind a tunnel with no
authentication can answer the reader's correspondence and cannot send mail to an address
that correspondence did not already carry. (Which addresses are the reader's own is the
mailbox's answer, not a setting here: docket reads the account's profile and its send-as
aliases, because mail is usually addressed to an alias rather than to the account's name.)
The one thing the reader decides about that audience is a **reply all** tick under the
field, on by default: clearing it answers the sender alone. It cannot do the opposite —
there is no value of it that puts an address on the reply that the message did not carry.

**A reply goes out in both forms.** The message is one `multipart/alternative`: the plain
text it has always been, and the same words as HTML beside it — the reader's paragraphs,
and the message being answered inside a `blockquote` so a client that renders markup does
not read the quote as part of the answer. The two parts are composed in one call
(`spec.ComposeReply`) from the same words and the same quote, so they cannot come to say
different things; nothing is converted from the other, and nothing in either is markup
that a person did not type. The preview shows the text part, which is the one that says
what the message says.
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