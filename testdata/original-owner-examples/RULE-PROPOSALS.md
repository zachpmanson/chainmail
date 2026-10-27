# Candidate rules — not implemented

The owner asked for anonymised red tests before changing the classifier. Both
fixtures require original HTML; the current receipts are 0 and 1 respectively.
No rule, threshold, sender preference or rendering behavior changes in this branch.

## 1. Recognize painted inline action links

Example 2 already earns main-layout evidence. Its action has its own background,
10px/28px padding and rounded corners, but no `display` declaration.
`originalanalysis.go` currently requires an anchor to declare `block`,
`inline-block` or `inline-flex` before considering its paint. An inline button is
therefore discarded even though its decoration is explicit.

**Proposed change:** allow compact, self-painted, generously padded anchors to
earn action evidence without an explicit block-like display. Keep nonempty,
non-contact destinations, local ownership of paint, primary-position and majority
coverage gates. Do not treat any colored or underlined link as a button.

**Diagnostic:** adding only `display:inline-block` to this fixture's painted
anchor changes its receipt from layout-only score 1 to layout+action score 2 and
original=true. This edits test markup only to isolate the gate; it is not a fix
and does not establish that every inline-anchor rule would be safe.

**Required negatives:** colored references; highlighted text; paint inherited from
a whole page cell; transparent/zero borders; a small trailing signature/profile
button after long correspondence. The existing negative suites cover several of
these and must remain unchanged.

## 2. Recognize coherent fluid table pages without inventing pixel bounds

Example 1 has a centered auto-width outer table, percentage-width child tables,
side gutters, a contrasting leading title, and an independently bordered/padded
module. None of the large primary regions satisfies the current main-layout gate:
percentage widths are not numeric bounds, gutters are not padding, and the banner
container's max-width of 1400 exceeds the current 320–1200 bound. A small inner
card cannot stand in for the rest of the message under the coverage rule.

**Candidate to prototype:** treat a coherent centered single-column fluid table
scaffold, with independently evidenced heading/module structure, as main layout.
Require a substantive shared primary region; neutral table nesting, centering or
100% width alone must remain insufficient. Specify the evidence independently to
avoid counting the same heading/module twice as both layout and design.

**Diagnostic:** adding only a 600px width to the outer table changes the fixture
from score 0 to score 3 (layout, hierarchy, graphic) and original=true. That proves
the main-geometry gate matters; it does not justify inserting a width, raising the
ceiling indiscriminately or pretending that every percentage image has a known
width. Unknown image geometry should remain unknown.

**Required negatives:** centered ordinary correspondence with a boxed aside;
nested/sibling data tables; a small signature card after long prose; disconnected
heading/action regions; neutral wrapper inflation; full-width tracking pixels.
This candidate is broader/riskier than the inline-action change.

Example 1 also has an anchor around a painted descendant control. Supporting that
shape would require a separate tightly owned compact-control rule, not arbitrary
paint propagation. It is not necessary to conflate it with fluid-layout detection.

## Validation before adopting either

Keep the main-layout-plus-independent-family requirement, uncertainty handling,
resource budgets and 50% weighted coverage policy. Keep sender/URL/class names and
fixture identities out of the detector. Run synthetic negatives, all 119 existing
corpus mutation checks, and the two new owner examples. The existing strict
corpus command still exposes its two previously owner-paused expectations; do not
silently relabel them or treat them as newly introduced failures.

These diagnoses use sanitized endpoint HTML. It reproduces the raw endpoint
receipts for these two cases, but raw stored-input fixtures remain unavailable.
