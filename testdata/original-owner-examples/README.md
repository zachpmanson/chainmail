# Owner-confirmed original-view examples

Two additional examples the owner explicitly said should default to original HTML
on 2026-10-27. Their desired labels are asserted directly by
`TestOriginalAnalysisOwnerExamples`; both now pass through generic structural
rules described in [RULE-PROPOSALS.md](RULE-PROPOSALS.md).
These are not additional temporarily paused corpus expectations. At collection,
the anonymised fixtures reproduced the endpoint's two receipts, and private
pre-anonymisation endpoint snapshots produced the same receipts too. This was
observed agreement on these examples, not a general guarantee of raw-input
equivalence.

## Provenance and anonymisation

These snapshots come from the read-only `/original?analysis=1` endpoint's **rendered,
sanitized HTML**, not the raw stored `body_html` analysed by the endpoint. Raw
service-owned corpus access was unavailable. Do not describe them as byte-faithful
raw MIME parts or use them to prove raw-input parity. The endpoint's original
analysis receipts were score 0 / no matches for example 1 and score 1 / main layout
only for example 2; both chose simplified without uncertainty.

Every text token is replaced by `word`, retaining token count but not word lengths.
Resource destinations use `https://example.invalid/resource-N` or opaque
`mailto:person-N@example.invalid` placeholders,
classes and IDs with opaque counters, font families with `sans-serif`. Comments,
stylesheets, titles, alt text and other metadata are removed. Inline geometry,
colours, table nesting and image dimensions remain. No source-message IDs,
identity mapping, medical text or private destinations are committed.

The tests also check receipt invariance under opaque identity renaming, neutral
wrapping, and appended marked history/signatures. Labels are test expectations,
never classifier inputs. The follow-up adds fluid-gutter layout and self-painted
inline-control evidence, without changing saved sender preferences or the
original renderer. A separate sanitizer fix flattens `picture` to its sanitized
fallback descendants in simplified mode; `source` and `srcset` remain excluded.

Run the regressions:

```sh
nix develop --command go test ./internal/spec -run TestOriginalAnalysisOwnerExamples -count=1 -v
```

Current receipts are score 3 (layout, hierarchy, graphic) and score 2 (layout,
action), both original without uncertainty. The full existing corpus receipts
are unchanged, including the two independently owner-paused expectations.
