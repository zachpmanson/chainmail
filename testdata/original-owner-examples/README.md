# Owner-confirmed original-view examples

Two additional examples the owner explicitly said should default to original HTML
on 2026-10-27. Their desired labels are asserted directly by
`TestOriginalAnalysisOwnerExamples`; the current detector is expected to fail.
These are not additional temporarily paused corpus expectations. The anonymised
fixtures reproduce the endpoint's two receipts, and private pre-anonymisation
endpoint snapshots produced the same receipts too. This is observed agreement on
these examples, not a general guarantee of raw-input equivalence.

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
never classifier inputs. Adding these tests does not change detection, saved
sender preferences, sanitization or rendering.

Run the deliberately red regressions:

```sh
nix develop --command go test ./internal/spec -run TestOriginalAnalysisOwnerExamples -count=1 -v
```

Keep these tests on an unmerged draft until a separately agreed rule change makes
them pass. Do not skip them or merge a red suite onto the deployment branch.
