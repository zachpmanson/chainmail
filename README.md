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
- [ ] View components + stylesheet
- [ ] `chainmail render spec.json -o page.html` — one self-contained file
- [ ] App shell: load a spec, switch views
- [ ] Later: persistence and semantic search across trails

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

```bash
npm install
npm run dev        # vite, against fixtures/synthetic.json
npm test           # vitest
npm run typecheck
npm run gen:types  # regenerate src/lib/spec.d.ts from the schema
```

`fixtures/` holds a synthetic trail covering the timezone and notice cases, and a
1-entry degenerate case. Real mail trails are sensitive and never committed: keep
them as `fixtures/*.local.json`, which is gitignored.
