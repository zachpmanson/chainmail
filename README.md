# Chainmail

Read an email trail as one chronological transcript, with its reply structure made
visible.

A mail thread hides its own history: a forward can carry months of exchange that
never appears as separate messages. Chainmail takes a trail **unspooled** into a flat
list of entries — every quoted message broken out with provenance — and renders it as
a navigable transcript: chronological by absolute time, annotated with who replied to
what, the chains it is made of shown as lanes.

Mail and Slack are slurped into one SQLite corpus, searched lexically and
semantically, curated, and rendered as a self-contained page. The renderer was ported
out of a 1300-line Python script into real files with real tests; structural parity
against a real 58-entry trail matches 25 of 25 counted properties.

## Install

`corpus` owns the database and every operation on it; `chainmail-server` exposes the
read paths over HTTP *and* serves the web client from the same loopback port (the
client is embedded with `go:embed`).

```bash
nix develop      # go, node 22, npm — or bring your own
make install     # builds corpus and chainmail-server into ~/.local/bin
npm install      # JS dependencies, from the committed lockfile
```

`make help` lists every operating command.

## Setup

Only Gmail is required: without Slack you have a mail corpus, without ollama you have
lexical search.

**1. Gmail.** Mail is read through [`docket`](https://github.com/zachpmanson/docket)'s
library in this process — chainmail keeps its own OAuth grant and reads the mailbox
directly, so no other principal sits in the data path (`-backend gmail`, the default).
Provisioning is the app's own **Sign in with Google** bar; the next `corpus slurp`
reads mail as that account. The older `-backend docket` shells out to the docket CLI
instead.

**2. Slack, via [slackdump](https://github.com/rusq/slackdump).** Slack's app limit is
per workspace, so a workspace at its cap has no slot for a reader app — slackdump
needs no app at all. Its browser login drives a bundled Firefox that a
package-managed build usually has not downloaded, so import the credentials your
browser already holds:

- **token** — devtools console on a logged-in Slack tab:
  `JSON.parse(localStorage.localConfig_v2).teams` → your workspace's `token` (`xoxc-…`)
- **cookie** — devtools → Application (Chrome) or Storage (Firefox) → Cookies →
  `https://app.slack.com` → the cookie named `d` (`xoxd-…`). `HttpOnly`, so the
  console cannot read it.

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

Without `KEEP_ALIVE` the model unloads after five idle minutes and the next search
pays a cold load.

**4. The corpus.**

```bash
export CHAINMAIL_CORPUS=~/.local/state/chainmail/corpus.db   # this is the default
export CHAINMAIL_ME=you@example.com                          # marks your own messages

corpus init
corpus slurp -since 2026-08-01   # slack, then mail, then settle, then embed
make doctor                      # what is in it, and what is missing
```

`slurp` is the whole sequence and safe to re-run: ingest is keyed on a content hash,
and mail is paged with a cursor that resumes rather than repeats. `-only`/`-skip`
choose phases; a phase whose prerequisite this host lacks is reported as a skip, not a
failure. The `settle` phases collapse duplicates and repair identities — `twins` and
`repair` refuse rather than guess, and `dedupe` prints a **dry run**, because those
merges weigh evidence and cannot be undone:

```bash
corpus dedupe -apply     # back up first: make backup
```

Every slurp re-probes the backends and rewrites the connection snapshot beside the
corpus, so the /status screen is never staler than the last slurp.

## Running it

```bash
chainmail-server &      # 127.0.0.1:8765, loopback only
npm run dev             # vite, proxying /v1 to the server
```

Search, tick the chains that belong, build a page from them.

`/status` shows which backends this machine is logged into (`make status`). The server
is read-only and never contacts Gmail or slackdump, so it reports whatever the
operator's last probe wrote; before the first probe every backend reads *unchecked*
rather than erroring. It refuses a non-loopback bind before opening the database —
this is personal mail, and a spec carries the sender's own HTML unsanitised
(issue #14).

Without the server, a spec on disk still renders:

```bash
make page Q="the query"                       # writes a spec and a page
corpus spec -q "…" -o spec.json               # or by hand
npm run render -- spec.json -o page.html
```

**Timeline** is one chronological column. **Columns** gives one lane per reply chain,
where the grid row is still the chronological index, so reading down stays in time
order; lanes are recycled once a chain ends. The **reply tree** panel indexes the
trail by structure — down is time, across is lane allocation — and lights the
ancestry of the entry you are reading, which is the one thing a chronological
transcript cannot show.

## Re-running

Every rendered page embeds the spec that produced it, so a later pass reloads
structured input instead of scraping HTML:

```bash
corpus refresh page.html -o new.json     # what has arrived since
render new.json -o page.html --since page.prev.html
```

`--since` reports what is **new** (no counterpart last pass) and **revised** (same
anchor, changed words, or the same words at a corrected timestamp); diffing a page
against itself reports nothing.

`corpus refresh` runs two passes, because the spec records two kinds of thing:
`threads` is membership, so every recorded chain is regenerated whole (a reply carrying
none of the query's words, "sounds good, Friday then", arrives only this way);
`queries` is discovery, and chains the page lacks are printed as proposals rather than
included — `-include-new` takes all, `-accept <root>` takes one — so a curated page
cannot re-widen on every refresh. A chain the queries no longer return stays on the
page and is reported: dropping it would delete permalinked entries, and `--since`
marks what is new and revised, not what is gone. The mailbox is only touched with
`-fetch` (each query narrowed to `after:` the previous run's date, each mail thread by
id); otherwise the corpus alone is read. Per-pass lines print either way, so "nothing
new" is legible as proof it looked.

`make repage P=page.html` runs the whole loop.

## The contract

Input is a **timeline spec**: JSON conforming to `schema/timeline.schema.json`. A
collector produces it (today the `mail-timeline` Claude Code skill); chainmail only
renders. That boundary is the point — collection needs judgement about what a mail
trail means, rendering does not. Two fields carry the most weight: **`parent`**, the
id each entry replies to, which drives ordering, lanes, the reply tree and the reply
links; and **`tz`**, the zone the source stated, since ordering is by *absolute* time
and a 09:51 NZST send correctly precedes a 09:20 AEST reply the same morning. A missing
zone is inferred from what that sender stated elsewhere, marked as inferred, and never
allowed to invert a reply chain.

## Development

A flake provides the toolchain, pinned to the machine config's nixpkgs channel; JS
dependencies still come from `npm install` against the committed lockfile.

```bash
nix develop          # go, node 22, npm, typescript-language-server
make check           # go test + vet + gofmt, vitest, typecheck — everything
make test            # the two test suites alone
npm run gen:types    # regenerate src/lib/spec.d.ts from the schema
npm run gen:api      # regenerate src/lib/api.d.ts from openapi.json
```

The Go side has **no direct dependencies** — everything in `go.mod` is `// indirect`,
and the HTTP service is `net/http` and `encoding/json`. The JS side has three at
runtime: `react`, `@tanstack/react-query` and `openapi-fetch`. Keep it that way unless
a dependency earns itself. The two generated files are never hand-edited, and a test
asserts the service's inlined copy of the timeline schema has not drifted.

`fixtures/synthetic.json` is a full-complexity trail — 58 entries across 7 chains
sharing 4 lanes, 51 reply edges, 37 stated and 18 inferred timezones — mirroring a real
trail's structure with the content rewritten, so the renderer is exercised at real
scale by a file safe to commit. `fixtures/minimal.json` is the 1-entry degenerate case.
Real trails are sensitive: keep them untracked at `fixtures/local.json`, loaded with
`?spec=`.

`corpus eval` scores two retrieval configurations over one judged query set and prints
the delta, because a retrieval number on its own says nothing:

```bash
corpus eval -set fixtures/eval.local.json \
  -a "name=lexical,mode=lexical" -b "name=hybrid,mode=hybrid" -cases
```

Each `-a`/`-b` spec takes `name db mode model url dim topk minsim noprefix`; `db` lets
the two configurations search different corpora, which is what makes a change to
*stored* vectors measurable. `-floor` prints the two cosine distributions a similarity
floor has to separate. Judged sets over real correspondence stay untracked at
`fixtures/eval.local.json` — even with no body text, query plus `ext_id` is a labelled
index of an inbox. `fixtures/eval.synthetic.json` is the committed example.
