# Implemented structural rules

The owner first requested anonymised failing tests, then authorized generic rule
changes. Both owner-confirmed examples now pass, including the identity, neutral
wrapper and marked-history/signature mutations. The original receipts were 0 and
1; they are now 3 (layout, hierarchy, graphic) and 2 (layout, action). No identity
exceptions, sender preferences or original-rendering changes were introduced.

## 1. Painted inline action links

Example 2 already earned main-layout evidence. Its action has its own background,
10px/28px padding and rounded corners, but no `display` declaration. Previously an
anchor had to declare `block`, `inline-block` or `inline-flex` to earn action
evidence.

The additional rule accepts implicit or explicit inline anchors with 1–12 words,
no images (their compactness is not assumed), their own nontransparent background
or visible border, at least 8px top/bottom padding and at least 16px left/right
padding. Shorthand (one to four values) or physical longhands are supported;
mixed shorthand/longhand declarations conservatively do not qualify because their
cascade is not resolved here. Padding accepts nonnegative px/pt lengths and em
lengths using the resolved element font, plus unitless zero. Positive unitless,
invalid, percentage, root-relative and logical padding are declined locally;
other existing scoring rules' length handling is unchanged.

Background paint supports valid nontransparent hex colors and basic named colors
(the sixteen HTML colors, orange and rebeccapurple). Functional colors and
multi-component shorthands are conservatively declined, as are mixed background
shorthand/longhand declarations regardless of order or importance. This avoids
guessing resets or treating `none no-repeat` as paint without introducing a CSS
parser. An independent visible border can still supply paint. Border-only paint
needs a visible line style in the shorthand and no unresolved border longhands.
Existing URL/contact,
primary-position, uncertainty and weighted-majority gates still apply. Paint is
not inherited from a page cell or propagated from an arbitrary descendant.

Synthetic negatives cover colored references, small highlights, rounded-only
highlights, one-axis padding, inherited-cell paint, transparent paint/borders,
zero/overridden/inherited/color-only borders, long reference text,
missing/contact destinations, mixed padding/background cascade, nonpainting and
unsupported background shorthands, invalid colors, inherited small-font em
padding, invalid/unitless/negative/unresolved lengths, descendant paint, unknown
image geometry and a small trailing profile control after long correspondence.
Positive checks retain independently painted borders, named colors, px/pt and
font-resolved em padding. Existing block-like action behavior is unchanged.

## 2. Coherent fluid table gutters

Example 1 has a centered auto-width outer table, full-percentage-width child
tables, repeated side gutters, a contrasting leading title and a bordered/padded
module. Previously no large primary region satisfied the main-layout gate:
percentage widths are not numeric bounds, gutters are not padding, and the banner
container's max-width of 1400 exceeds the existing 320–1200 bound.

The new geometry rule requires a `width:100%` table with at least two nonempty
rows sharing exactly three cells: a central content cell between blank,
image-free side cells of equal explicit width, 12–48px. The gutter width must
repeat across all content rows. At least one central cell must contain more than
20 words. Content-bearing spanning/extra cells or captions disqualify the
scaffold. Every row is checked for rowspans and physical column count, including
empty rows: all nontrivial rowspans are declined. Empty rows may be empty, a
single full-width `colspan="3"` table cell, or the same three-cell gutter shape;
they cannot establish repetition. Nested tables' rows do not contribute to the
parent scaffold.

Explicit display overrides must preserve table, row-group, row and central-cell
roles. Blank gutter cells may also use `display:block`, which sits inside an
anonymous table cell; this owner shape is not rejected indiscriminately. Gutter
CSS widths use valid local lengths, not positive unitless CSS numbers. Explicit
min/max widths must resolve and agree with the gutter width. Nonzero horizontal
padding, nonzero table cellpadding, unresolved padding, borders/margins requiring
additional sizing and logical sizing overrides are conservatively declined.
These checks do not attempt general table layout or intrinsic sizing.

The table must be centered itself or through an ancestor scaffold containing no
other retained text area or images. The geometry establishes layout only, without
reading headings, actions or modules. The existing independent-family and 50%
weighted-area gates then apply to that same candidate region. No heading is used
to establish the fluid geometry, and no pixel width is assigned to the column or
its images. The pre-existing image resolver and maximum-width ceiling are
unchanged; example 1's graphic vote is from that existing resolver, not inferred
from its gutters.

Synthetic negatives cover width/centering alone, a single guttered row, empty
repetition, asymmetrical/changing/percentage/tiny gutters, populated side columns,
spanning/unaligned content, empty-row rowspans/extra columns, contradictory
min/max/overridden gutter widths, gutter padding/cellpadding, invalid CSS widths,
block/grid table or block row/group/central-cell overrides, unrelated centered
ancestors, nested-row pooling, ordinary data, a lone boxed aside, disconnected
heading/action regions and a small late fluid card after long prose. Positive
checks retain full-width empty spacers and anonymous blank block gutters. Separate tests show that a qualifying
fluid scaffold without an independent family stays simplified and unknown-width
images/tracking pixels do not supply that family.

A link around a painted descendant control remains outside the new inline rule;
arbitrary paint propagation was not added to make example 1 pass.

## Validation and limits

All 119 existing corpus receipts remain byte-for-byte equal to the pre-change
receipts, including the two previously owner-paused expectations. All corpus
mutation checks and 117 active expected-mode assertions pass. Strict mode still
reports only the same two paused false negatives; their labels and pause are
unchanged. Focused synthetic regressions, the two owner examples and the full
`internal/spec` suite pass.

These are conservative structural estimates, not computed browser geometry or
unseen-template validation. The new fluid rule intentionally declines other
percentages, nonrepeated gutters, unknown gutter dimensions and unsupported
paint/length/cascade or grid overrides as described above. Compact,
generously padded self-painted references are structurally indistinguishable
from buttons; locality and coverage gates still limit their influence.

These diagnoses use sanitized endpoint HTML. It reproduces the raw endpoint
receipts for these two cases, but raw stored-input fixtures remain unavailable.
This does not establish general raw-input parity.
