# Chainmail

Read an email trail as one chronological transcript, with its reply structure made
visible.

A mail thread hides its own history: a single forward can carry months of exchange
that never appears as separate messages in a mailbox. Chainmail takes a trail that
has been **unspooled** into a flat list of entries — every quoted message broken out
as its own entry, with provenance — and renders it as a transcript you can navigate:
chronological by absolute time, annotated with who replied to what, and with the
chains it is actually made of shown as lanes.

## Status

Working end to end: mail and Slack are slurped into one SQLite corpus, searched
lexically and semantically, selected from, and rendered as a transcript. The
renderer was ported out of a single 1300-line Python script (half of which was CSS
and JS trapped in string literals) into real files with real tests.

- [x] `schema/timeline.schema.json` — the spec contract, versioned
- [x] Generated TypeScript types + a normaliser that still accepts legacy specs
- [x] Ordering, lane allocation and chain/meta classification, with tests
- [x] View: transcript, chain columns, reply-tree minimap, participants and
      sources panels, reply links, permalinks
- [x] `render <spec> -o page.html [--since prev.html]` — one self-contained file
- [x] Dev app: load a spec by path, URL or drag-and-drop
- [x] Corpus persistence: every message from every source, with the reply graph
      materialised, and lexical search over two FTS5 tokenizers
- [x] Semantic search: one vector per entry via a local model, fused into the
      lexical ranking. `corpus embed` fills them in; `corpus search -mode
      semantic|hybrid` uses them. Needs `ollama pull nomic-embed-text` — the
      corpus is personal mail, so nothing is sent anywhere
- [x] Retrieval evaluation: `corpus eval` scores two configurations over one
      judged set of queries, so a change to indexing or ranking is measured
      rather than argued about
- [x] Quoted-block extraction: a message quoted inside a forward becomes its own
      entry, deduped across every place it was seen, with its own markup recovered
      from whichever host message carried it
- [x] Identity: one person across several addresses, a rebrand, plus-addressing and
      Slack, with every merge audited and ambiguity refused rather than guessed
- [x] `corpus refresh <spec|page>` — re-run a page's own queries and chains, apply
      what belongs to chains already on it, propose what is new
- [x] HTTP service (`chainmail-server`, loopback only) described by
      `api/openapi.json`, and a React client that searches, selects and builds

Structural parity with the original Python renderer is verified against a real
58-entry trail: 25 of 25 counted properties match (entries, chains, spines, reply
links, permalinks, timezone labels, minimap nodes, participants, panels, diff
marks), with no dangling internal links.

## Install

Two binaries and a web app. `corpus` owns the database and every operation on it;
`chainmail-server` exposes the read paths over HTTP AND serves the web client
(from the same port — the client is embedded into the binary via go:embed, so a
single `chainmail-server` process is the whole product over one loopback port).

```bash
nix develop              # go, node 22, npm — or bring your own
make install             # builds corpus and chainmail-server into ~/.local/bin
npm install              # JS dependencies, from the committed lockfile
```

`make help` lists every operating command. Nothing below needs the flake if you
already have Go and Node on the same versions.

## Setup

Four external pieces, in the order they are needed. Only the first is required —
without Slack you have a mail corpus, and without ollama you have lexical search.

### 1. docket — mail

[`docket`](https://github.com/zachpmanson/docket) is the Gmail CLI. Chainmail
shells out to it rather than holding OAuth credentials of its own, so authenticate
there once and chainmail inherits it. It must be on `PATH`.

Chainmail needs a build that exposes threading headers, the HTML part, and
attachment bytes; `corpus ingest mail` fails closed if the threading headers are
missing rather than silently building a corpus with no reply graph.

### 2. slackdump — Slack

Slack's app limit is per workspace, so on a workspace at its cap there is no slot
left to create a reader app in. [`slackdump`](https://github.com/rusq/slackdump)
needs no app at all.

Its browser login drives a bundled Firefox that a package-managed build usually
has not downloaded, in which case authentication fails with a misleading
"workspace not found". Importing the credentials your browser already holds avoids
it entirely:

- **token** — devtools console on a logged-in Slack tab:
  `JSON.parse(localStorage.localConfig_v2).teams` → your workspace's `token`
  (`xoxc-…`)
- **cookie** — devtools → Application (Chrome) or Storage (Firefox) → Cookies →
  `https://app.slack.com` → the cookie named `d` (`xoxd-…`). It is `HttpOnly`, so
  the console cannot read it; it has to come from that panel.

```bash
printf 'SLACK_TOKEN=%s\nSLACK_COOKIE=%s\n' "$TOKEN" "$COOKIE" > slack.env
slackdump workspace import slack.env && rm slack.env
slackdump archive -o ~/.local/state/chainmail/slack
```

Delete the env file afterwards: it is a full-read credential for the whole
workspace, DMs included. `slackdump resume <dir>` is incremental from then on, and
credentials persist until the cookie expires — months, typically, but it does.

### 3. ollama — semantic search

Needed at **query time**, not only when indexing: a search embeds the query, so the
daemon must be running to use `-mode semantic` or `hybrid`. Lexical search needs
nothing.

```bash
OLLAMA_KEEP_ALIVE=-1 ollama serve &     # -1 keeps the model resident
ollama pull nomic-embed-text
```

Without `KEEP_ALIVE`, ollama unloads an idle model after five minutes and the next
search pays a cold load of a few seconds.

### 4. The corpus

```bash
export CHAINMAIL_CORPUS=~/.local/state/chainmail/corpus.db   # this is the default
export CHAINMAIL_ME=you@example.com                          # marks your own messages

corpus init
corpus slurp -since 2026-08-01   # slack, then mail, then settle, then embed
make doctor                      # what is in it, and what is missing
```

`corpus slurp` is the whole sequence, so a host with the binaries and no checkout
runs it too — `make slurp` is a call to it. It is safe to re-run: ingest is keyed on
a content hash, so an unchanged message is skipped, and mail is paged to the end of
its query with a cursor that resumes rather than repeats. `-only` and `-skip` choose
phases (`-skip slack` for a mailbox-only corpus, `-skip embed` when ollama is not
up), and a phase whose prerequisite this host does not have is reported as a skip
rather than failing the run, so only a real breakage exits non-zero.

The `settle` phases collapse duplicates and repair identities. `twins` and `repair`
refuse rather than guess, then `dedupe` is printed as a **dry run** — those merges
weigh evidence and cannot be undone, so applying them is a separate decision, and
no `slurp` flag will do it for you:

```bash
corpus dedupe -apply
```

Back the database up first (`make backup`) if you are going to.

## Running it

```bash
chainmail-server &      # 127.0.0.1:8765, loopback only
npm run dev             # vite, proxying /v1 to the server
```

Then search, tick the chains that belong, and build a page from them.

The server refuses a non-loopback bind before it even opens the database. This is
personal mail, and a spec carries the sender's own HTML unsanitised — see the note
on the bind, and issue #14.

To work without the server, a spec on disk still renders directly:

```bash
make page Q="the query"                       # writes a spec and a page
corpus spec -q "…" -o spec.json               # or by hand
npm run render -- spec.json -o page.html
corpus refresh spec.json -o spec.json         # pull in anything new since
```

## Views

**Timeline** — one chronological column. **Columns** — one lane per reply chain,
where the grid row is still the chronological index, so reading down stays in time
order while each chain keeps its own lane. Lanes are recycled once a chain ends, so
a dead chain does not hold a column open.

The **reply tree** panel indexes the whole trail by structure: down is time, across
is only lane allocation. It scroll-spies the entry you are reading and lights its
ancestry back to the chain start, which is the one thing a chronological transcript
cannot show.

## Re-running

Every rendered page embeds the spec that produced it, so a later pass reloads exact
structured input instead of scraping HTML:

```bash
corpus refresh page.html -o new.json     # what has arrived since
render new.json -o page.html --since page.prev.html
```

`render --since` reports what is **new** (no counterpart in the previous pass) and
what is **revised** (same anchor, changed words — or the same words at a corrected
timestamp, which would otherwise read as one deleted plus one new). Diffing a page
against itself reports nothing, so a re-run that claims changes means the input
really changed.

`corpus refresh` produces the input for that, from a spec or from a page. It has
two passes over two different kinds of thing, because the spec records both:

- **`threads` is membership.** A chain list is a decision — search, then check off
  what belongs — so every recorded chain is regenerated whole. This is the half
  that matters: a reply carrying none of the query's words ("sounds good, Friday
  then") is invisible to any search and arrives only this way.
- **`queries` is discovery.** Re-running them finds chains the page does not have,
  which are printed as proposals and left out. `-include-new` takes all of them,
  `-accept <root>` takes one by the chain root the report names. Including them
  automatically would let a curated page re-widen on every refresh, which would
  make the curation meaningless.

A chain the queries no longer return stays on the page and is reported, since
dropping it would delete entries somebody has already read and permalinked — and
`--since` would not say so, because that diff marks what is new and revised, not
what is gone.

The mailbox is not touched unless you ask. Filling the corpus is `corpus ingest`'s
job, and a refresh against the corpus alone already picks up whatever a later
ingest brought in. `-fetch` adds a pass that asks: each query narrowed to `after:`
the previous run's date, and each mail thread by id with no date bound at all,
since that call returns every envelope anyway and the ids are checked against the
corpus before anything is read.

Nothing-new is an outcome, not a silence. The per-pass lines print either way —
what was asked, how many envelopes came back, how many were already stored — so
"nothing new" is legible as proof it looked rather than as a refresh that broke.

`make repage P=page.html` runs the whole loop.

## The contract

Input is a **timeline spec**: JSON conforming to `schema/timeline.schema.json`. A
collector produces it (today, the `mail-timeline` Claude Code skill, which searches
the mailbox and unspools quoted chains); chainmail only renders. That boundary is
the point — collection needs judgement about what a mail trail means, rendering does
not.

The two fields that carry the most weight:

- **`parent`** — the id of the entry each entry replies to. It drives ordering,
  lane allocation, the reply tree and the reply links. Coverage here is what makes
  the rest work.
- **`tz`** — the zone the source stated. Ordering is by *absolute* time, so a
  09:51 NZST send correctly precedes an 09:20 AEST reply the same morning. Where a
  zone is absent one is inferred from what that sender stated elsewhere, marked as
  inferred in the output, and never allowed to invert a reply chain.

## Development

A flake provides the toolchain, pinned to the same nixpkgs channel as the machine
config so it does not drift a release ahead of the system:

```bash
nix develop          # go, node 22, npm, typescript-language-server
direnv allow         # or enter it automatically on cd
```

Nix supplies the toolchain only; JS dependencies still come from `npm install`
against the committed lockfile.

```bash
make check         # go test + vet + gofmt, vitest, typecheck — everything
make test          # the two test suites alone

npm install
npm run dev        # vite, against fixtures/synthetic.json unless ?spec= says otherwise
npm run gen:types  # regenerate src/lib/spec.d.ts from schema/timeline.schema.json
npm run gen:api    # regenerate src/lib/api.d.ts from api/openapi.json
```

The Go side has **no direct dependencies** — everything in `go.mod` is `// indirect`,
and the HTTP service is `net/http` and `encoding/json`. The JS side has three at
runtime: `react`, `@tanstack/react-query` and `openapi-fetch`. Keep it that way
unless a dependency earns itself.

Two generated files, neither hand-edited: `src/lib/spec.d.ts` from the timeline
schema, and `src/lib/api.d.ts` from the OpenAPI document. The service inlines the
timeline schema into its own document, and a test asserts the copy has not drifted —
so the spec has one definition even though two documents describe it.

`fixtures/synthetic.json` is a full-complexity trail: 58 entries across 7 chains
(sharing 4 lanes), 51 reply edges, 37 stated and 18 inferred timezones, 3 meeting
notices, 11 @mentions, 6 tables, 37 internal cross-links, 5 avatars, 15
participants and 17 open items. It mirrors a real trail's structure exactly —
same timestamps, same reply graph, same interlinking — with the cast and content
rewritten, so the renderer is exercised at real scale by a file that is safe to
commit. `fixtures/minimal.json` is the 1-entry degenerate case.

Real mail trails are sensitive and are never committed. Keep them untracked at
`fixtures/local.json` and load them with `?spec=`.

### Measuring retrieval

`corpus eval` scores two retrieval configurations over one set of judged queries
and prints the delta, because a retrieval number on its own says nothing:

```bash
corpus eval -set fixtures/eval.local.json \
  -a "name=lexical,mode=lexical" \
  -b "name=hybrid,mode=hybrid" -cases
```

Each `-a`/`-b` spec takes `name db mode model url dim topk minsim noprefix`.
`db` lets the two configurations search different corpora, which is what makes a
change to *stored* vectors measurable: prefixed and unprefixed documents cannot
coexist under one model name, so the comparison is between two copies. `-floor`
prints the two cosine distributions a similarity floor has to separate — the
scores of judged-correct results against the scores of queries the corpus cannot
answer — which is how the floor in `embed.Traits` was chosen.

The judged set that means anything is judgements about real correspondence, so it
stays untracked at `fixtures/eval.local.json`; a query is a paraphrase of what a
thread is about and an `ext_id` is a durable pointer into a real mailbox, so the
pair is a labelled index of somebody's inbox even with no body text in it.
`fixtures/eval.synthetic.json` is the committed example, judged against a corpus
`internal/eval`'s tests build, and it carries the mechanical tests.
