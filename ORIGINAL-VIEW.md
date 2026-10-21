# Original HTML default

`internal/spec/originalanalysis.go` owns the one classifier. The browser uses its
receipt for both automatic selection and the expanded-header explanation; it does
not score the displayed HTML again. Sender/server or browser-saved preferences
still override the automatic choice.

## Analysis, not rewriting

Analysis parses a separate copy of the entry's own stored HTML. Neither the stored
part nor `OriginalBody` changes. The ordinary original endpoint remains sanitized
`text/html`; `?analysis=1` returns `{html, analysis}` with exactly the same fragment
and `Cache-Control: no-cache`. No second HTML copy is sent for scoring. OpenAPI and
the generated client types document this opt-in response.

Before scoring:

- Executable/dead content is pruned using the original-rendering policy. Inline
  `display:none`, hidden visibility/attributes, zero opacity, tracker-sized images,
  and zero-font text are ignored. Visible child font sizes inside zero-font layout
  wrappers survive. Stylesheets are not interpreted or fetched.
- Explicit quote containers are excluded using the existing backend boundary
  recognition, including normalized client class tokens and Apple cite blocks.
  Gmail signatures and existing `details.sig` folds are excluded. Removing one
  container never cuts its following siblings. Unmarked pull quotes remain.
- Header-only or unwrapped history uses shared `isBoundary`/`boundarySpans`
  recognition, not another header vocabulary. It remains in the analysis tree
  with `uncertain=true`, which blocks automatic original. This does not pretend to
  distinguish historical text from later inline answers.
- Compact contact/logo regions after earlier content are excluded by shape:
  contact-link schemes, an image, no prominent graphic/action, at most 120 words,
  and flows of at most 12 words. No names, sign-off phrases or generic footer IDs
  participate. Only the region is removed, not a suffix. No corpus repeated-tail
  lookup runs.

## Evidence

Each evidence family contributes one point, at most once in a candidate region:

1. **Page layout:** a fixed/max width of 320–1200 CSS pixels, or padding of at least
   16 pixels plus a painted/bordered surface. A centered painted table can obtain
   its column bound from a descendant cell/table.
2. **Designed action:** an explicitly block/inline-block/inline-flex link with its
   own painted/padded or rounded button geometry (including paint inside a link).
   An ordinary inline reference does not inherit the whole page cell's styling.
3. **Hierarchy/modules/columns:** semantic headings, short large-font blocks with
   actual size contrast, multiple independent painted/padded modules, or explicit
   CSS grid/flex columns. Uniform large body type and repeated nested wrappers do
   not create independent evidence. Ordinary data cells are not modules.
4. **Prominent graphic:** explicit image geometry (including a 400×90 banner), not
   an image count, linked logo count or tracking pixel.

Original requires the page-layout family **and at least one other family in the
same region**, with no uncertain history. Fully evidenced, non-overlapping regions
must cover at least half of retained text words. Multiple authored designed
sections can meet this coverage together, but partial votes from disconnected
regions cannot be pooled. A receipt reports only retained main-region evidence;
its score alone is not an unconditional selection rule.

No tag-count points, p-tag percentage guard, opaque class/ID/URL identity,
sender/service lists, semantic service categories, conversation state, fixture ID,
label or external corpus is an input. Equivalent p/div/br correspondence and
neutral wrappers do not manufacture design evidence. Inline colour, styling,
nested tables and data tables alone are not independent design families.

## Limits and current evaluation

This is a conservative structural heuristic, not a browser renderer or an
accuracy claim. External/class-based CSS, arbitrary CSS expressions, inherited
visibility overrides and intrinsic image dimensions are not resolved. Explicit
quote containers can themselves contain authored answers; HTML alone cannot
recover that authorship. Unmarked signatures/contact regions are necessarily
heuristic. The word-coverage gate can underweight a visually prominent short card
when a long legal footer follows it. Unknown cases remain simplified; the manual
original control remains available.

The 119 anonymized fixtures have no stylesheet bodies and provisional labels.
The strict test is intentionally **not skipped or relabelled**:

```sh
nix develop -c go test ./internal/spec -run TestOriginalAnalysisAnonymisedCorpus -count=1
```

Current result: **109/119 matches, 0 false positives, 10 false negatives**. The
following expected-original fixtures remain simplified:

- `thread-011-1`: short designed card loses text coverage to its trailing text.
- `thread-041-1`, `thread-059-1`: bounded prose notification with no independent
  retained action, heading, modules, columns or prominent graphic.
- `thread-044-1`, `thread-046-1`, `thread-056-1`, `thread-058-1`, `thread-061-1`,
  `thread-063-1`: main designed card does not dominate the retained text; trailing
  small-font material remains in the denominator.
- `thread-052-1`: dominant prose section has a layout but not another independent
  design family localized there.

These are genuine disagreements, not exceptions in the classifier. All 119
fixtures also retain identical receipts under opaque identity renaming, neutral
wrapping and addition of marked history/signatures. Synthetic tests cover the
three stress cases, independent-evidence negatives, hidden/tracker exclusion,
flat div designs, main design after closed quotes/contact regions, and retained
inline answers/pull quotes.

`npm test` passes (280 tests plus one pre-existing expected failure). Original
endpoint, classifier synthetic tests, lint, formatting, vet and typecheck pass.
`make check` currently fails at the strict corpus test on the ten disagreements
above; it must not be represented as a green full validation. No label changes
or fixture-specific exceptions were made. Resolve these limitations in independent
review before treating the branch as merge-ready.
