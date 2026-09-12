# Chainmail

Read an email trail as one chronological transcript, with its reply structure made
visible.

A mail thread hides its own history: a forward can carry months of exchange that never
appear as separate messages. Chainmail takes a trail **unspooled** into a flat list of
entries — every quoted message broken out with provenance — and renders it as a
navigable transcript: chronological by absolute time, annotated with who replied to
what, the chains it is made of shown as lanes.

Mail and Slack are slurped into one SQLite corpus, searched lexically and semantically,
curated, and rendered as a self-contained page.

## Install

`corpus` owns the database and every operation on it; `chainmail-server` exposes the
read paths over HTTP *and* serves the web client from the same loopback port (embedded
with `go:embed`).

```bash
nix develop      # go, node 22, npm — or bring your own
make install     # builds corpus and chainmail-server into ~/.local/bin
npm install      # JS dependencies, from the committed lockfile
```

`make help` lists every operating command.

## Setup

Four external pieces; only the first is required — without Slack you have a mail corpus,
without ollama you have lexical search.

**1. Gmail.** Mail is read in-process: chainmail keeps its own OAuth grant and reads the
mailbox directly. Provisioning is the app's own **Sign in with Google** bar; the next
`corpus slurp` reads mail as that account.

**2. Slack, via [slackdump](https://github.com/rusq/slackdump).** Slack's app limit is per
workspace, so a workspace at its cap has no slot for a reader app — slackdump needs none.
Its browser login drives a bundled Firefox a package-managed build usually has not
downloaded, so import the credentials your browser already holds:

- **token** — devtools console on a logged-in Slack tab:
  `JSON.parse(localStorage.localConfig_v2).teams` → your workspace's `token` (`xoxc-…`)
- **cookie** — devtools → Application/Storage → Cookies → `https://app.slack.com` → the
  cookie named `d` (`xoxd-…`). `HttpOnly`, so the console cannot read it.

```bash
printf 'SLACK_TOKEN=%s\nSLACK_COOKIE=%s\n' "$TOKEN" "$COOKIE" > slack.env
slackdump workspace import slack.env && rm slack.env   # full-read credential: delete it
slackdump archive -o ~/.local/state/chainmail/slack
```

`slackdump resume <dir>` is incremental from then on.

**3. ollama.** Needed at **query time**, not only when indexing — a search embeds its
query, so the daemon must be running for `-mode semantic` or `hybrid`.

```bash
OLLAMA_KEEP_ALIVE=-1 ollama serve &     # -1 keeps the model resident
ollama pull nomic-embed-text
```

**4. The corpus.**

```bash
export CHAINMAIL_CORPUS=~/.local/state/chainmail/corpus.db   # this is the default
export CHAINMAIL_ME=you@example.com                          # marks your own messages

corpus init
corpus slurp -since 2026-08-01   # slack, then mail, then settle, then embed
make doctor                      # what is in it, and what is missing
```

`slurp` is the whole sequence and safe to re-run: ingest is keyed on a content hash and
mail is paged with a resumable cursor. `-only`/`-skip` choose phases; a phase whose
prerequisite this host lacks is reported as a skip, not a failure. The `settle` phases
collapse duplicates and repair identities — `twins` and `repair` refuse rather than
guess, and `dedupe` prints a **dry run**:

```bash
corpus dedupe -apply     # irreversible; back up first with make backup
```

Every slurp re-probes the backends and rewrites the connection snapshot beside the
corpus, so /status is never staler than the last slurp.

## Running it

```bash
chainmail-server &      # 127.0.0.1:8765, loopback only
npm run dev             # vite, proxying /v1 to the server
```

Search, tick the chains that belong, build a page from them. `/status` reports whichever
backends the operator's last probe (`make status`) saw. The server refuses a non-loopback
bind before opening the database: this is personal mail, and a spec carries the sender's
own HTML unsanitised (issue #14).

Without the server, a spec on disk still renders:

```bash
make page Q="the query"                       # writes a spec and a page
corpus spec -q "…" -o spec.json               # or by hand
npm run render -- spec.json -o page.html
```

**Timeline** is one chronological column. **Columns** gives one lane per reply chain,
with the grid row still the chronological index, so reading down stays in time order.
The **reply tree** panel indexes the trail by structure and lights the ancestry of the
entry you are reading.

## Re-running

Every rendered page embeds the spec that produced it, so a later pass reloads structured
input instead of scraping HTML:

```bash
corpus refresh page.html -o new.json     # what has arrived since
render new.json -o page.html --since page.prev.html
```

`--since` reports what is **new** (no counterpart last pass) and **revised** (same
anchor, changed words, or the same words at a corrected timestamp).

`corpus refresh` restores every recorded chain whole — the only way a reply carrying none
of the query's words arrives — and re-runs the recorded queries, whose new chains are
printed as proposals rather than included (`-include-new`, or `-accept <root>` for one),
so a curated page cannot re-widen on every refresh. A chain the queries no longer return
stays on the page and is reported. The mailbox is only touched with `-fetch`.
`make repage P=page.html` runs the loop.

## The contract

Input is a **timeline spec**: JSON conforming to `schema/timeline.schema.json`. A
collector produces it (today the `mail-timeline` Claude Code skill); chainmail only
renders. Two fields carry the most weight. **`parent`**, the entry each entry replies to,
drives ordering, lanes, the reply tree and the reply links. **`tz`**, the zone the source
stated, matters because ordering is by *absolute* time: a 09:51 NZST send correctly
precedes a 09:20 AEST reply. A missing zone is inferred from what that sender stated
elsewhere and never allowed to invert a reply chain.

## Development

A flake provides the toolchain, pinned to the machine config's nixpkgs channel; JS
dependencies come from `npm install` against the committed lockfile.

```bash
nix develop          # go, node 22, npm, typescript-language-server
make check           # go test + vet + gofmt, vitest, typecheck — everything
npm run gen:types    # regenerate src/lib/spec.d.ts from the schema
npm run gen:api      # regenerate src/lib/api.d.ts from openapi.json
```

The Go side has **no direct dependencies** (all `// indirect`); the HTTP service is
`net/http` and `encoding/json`, and the JS side has three packages at runtime: `react`,
`@tanstack/react-query`, `openapi-fetch`. Keep it that way unless a dependency earns
itself. Generated files are never hand-edited, and a test asserts the service's inlined
timeline schema has not drifted.

`fixtures/synthetic.json` is a full-complexity trail — 58 entries, 7 chains sharing 4
lanes, 51 reply edges, 37 stated and 18 inferred timezones — a real trail's structure with
the content rewritten, so the renderer is exercised at real scale by a committable file.
Real trails are sensitive: keep them untracked at `fixtures/local.json`, loaded with
`?spec=`.

`corpus eval` scores two retrieval configurations over one judged query set and prints the
delta, because a retrieval number on its own says nothing:

```bash
corpus eval -set fixtures/eval.local.json \
  -a "name=lexical,mode=lexical" -b "name=hybrid,mode=hybrid" -cases
```

Each spec takes `name db mode model url dim topk minsim noprefix`; `db` lets the two
configurations search different corpora, which is what makes a change to *stored* vectors
measurable. Judged sets over real correspondence stay untracked at `fixtures/eval.local.json`;
`fixtures/eval.synthetic.json` is the committed example.