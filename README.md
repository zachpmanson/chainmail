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

Early. The renderer is being ported out of a single 1300-line Python script (half of
which was CSS and JS trapped in string literals) into real files with real tests.

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

Structural parity with the original Python renderer is verified against a real
58-entry trail: 25 of 25 counted properties match (entries, chains, spines, reply
links, permalinks, timezone labels, minimap nodes, participants, panels, diff
marks), with no dangling internal links.

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
render new.json -o page.html --since previous.html
```

That reports what is **new** (no counterpart in the previous pass) and what is
**revised** (same anchor, changed words — or the same words at a corrected
timestamp, which would otherwise read as one deleted plus one new). Diffing a page
against itself reports nothing, so a re-run that claims changes means the input
really changed.

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
nix develop          # node 22, npm, typescript-language-server
direnv allow         # or enter it automatically on cd
```

Nix supplies the toolchain only; JS dependencies still come from `npm install`
against the committed lockfile.

```bash
npm install
npm run dev        # vite, against fixtures/synthetic.json
npm test           # vitest
npm run typecheck
npm run gen:types  # regenerate src/lib/spec.d.ts from the schema
```

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
