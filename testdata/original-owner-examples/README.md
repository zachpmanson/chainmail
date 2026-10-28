# Owner-confirmed original-view examples

The first two examples were explicitly requested to default to original HTML
on 2026-10-27. Example 3 is an invented structural regression for the owner's
whole-message table-containment request in [#359](https://github.com/zachpmanson/chainmail/issues/359). Their desired labels are asserted directly by
`TestOriginalAnalysisOwnerExamples`; both now pass through generic structural
rules described in [RULE-PROPOSALS.md](RULE-PROPOSALS.md).
These are not additional temporarily paused corpus expectations. At collection,
the anonymised fixtures reproduced the endpoint's two receipts, and private
pre-anonymisation endpoint snapshots produced the same receipts too. This was
observed agreement on these examples, not a general guarantee of raw-input
equivalence.

## First two examples: provenance and anonymisation

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

Current receipts are score 4 (layout, hierarchy, graphic, table containment),
score 3 (layout, action, table containment), and score 2 (layout, table containment),
all original without uncertainty. The containment addition changes 48 of the 119
corpus receipts by exactly one point, with all prior matches unchanged. Only the
two independently owner-paused cases change default (simplified to original);
their labels and pause code are unchanged. Strict mode now passes all 119 labels.

## Invented whole-table example

Example 3 contains only synthetic wording and `example.test` resources. It is not
an anonymised copy of private sender HTML. It preserves a full-width outer table,
a bounded main table, a small CSS-sized logo with larger intrinsic dimensions,
a link whose paint belongs to its enclosing cell, and prose/footer inside the
outer table. Hidden preview text and an external tracking-size image do not vote.
It failed before the containment rule with score 1 (layout only), then passed
with score 2; neither its link nor logo gains an action/graphic vote.

The private motivating example's authoritative live receipt was computed on raw
input, whereas local follow-up analysis used sanitized display HTML. The local
result is not evidence of raw-input parity. Its explicit saved simplified sender
preference is separate from the classifier default and is neither changed nor
overridden by this rule. No private HTML, identities, wording or destinations are
included in this fixture.
