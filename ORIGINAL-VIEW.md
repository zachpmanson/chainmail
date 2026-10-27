# Original HTML default

`internal/spec/originalanalysis.go` owns the one HTML-only classifier. The browser
uses its receipt for automatic selection and the expanded-header explanation;
saved server/browser preferences still win. It never rescores the displayed HTML.

## Separate analysis

The stored part and `OriginalBody` rendering are unchanged. Ordinary GET remains
sanitized `text/html`; `?analysis=1` returns `{html, analysis}` with exactly the
same fragment and `Cache-Control: no-cache`. Analysis uses its own parsed tree,
not the application-generated canvas styles.

Before scoring:

- Ignore executable/dead content, inline hidden/zero-opacity content, explicit
  zero-size/tracker images and zero-font text. Visible child font sizes inside
  zero-font layout wrappers survive. Supported inline declarations honor
  `!important` and numeric-zero spellings.
- Reuse backend quote/header recognition. Explicit history containers, normalized
  Gmail signatures and `details.sig` folds are excluded locally; later authored
  siblings survive. Unmarked pull quotes remain. Ambiguous unwrapped history
  stays intact and makes the automatic decision uncertain/simplified.
- Exclude compact contact/logo regions after earlier content by logical flow,
  contact-link schemes and small-image geometry, not names, sign-off phrases or
  footer IDs. Paragraph/div/br equivalents agree; a later authored design is not
  cut away. Telephone scheme facts are retained only on the private analysis
  tree; the original-rendering URL policy is not relaxed.

## Independent evidence

Each family contributes at most one point in a candidate primary region:

1. **Main layout:** bounded/centered, padded, structured page geometry. A bare
   width declaration alone is insufficient. Aligned, same-width centered sibling
   panels can form one page column; disconnected partial regions cannot pool
   their votes. A centered 100%-width table can also establish layout through
   repeated equal blank 12–48px side-cell gutters around a substantive central
   column. All rows, including empty spacers, must preserve that grid: unresolved
   rowspans, contradictory table/row displays or gutter sizing/padding decline
   this added rule. Blank block-display gutters inside anonymous cells remain
   supported. This uses geometry only, not headings/modules or guessed image bounds.
2. **Designed action:** a block-like link with its own painted/padded or rounded
   button geometry, or a compact self-painted inline link with at least 8px
   vertical and 16px horizontal padding. This inline rule validates local CSS
   lengths (em uses the resolved font), rejects positive unitless padding and
   declines mixed background/padding cascades or unsupported background shorthand.
   It cannot inherit a whole page cell's paint. A tiny late control in a bare text column does not describe the main prose.
3. **Hierarchy/modules/columns:** short leading heading flows with relative type
   contrast, independent painted/padded modules, or multiple visible content
   items in a horizontal layout. Uniform large body text, empty flex/grid,
   ordinary data cells and repeated nested wrappers do not qualify.
4. **Prominent graphic:** explicit image geometry, including supported percentage
   widths within an explicitly bounded column. Unknown intermediate constraints,
   intrinsic image sizes and external CSS are not guessed. Small logos and
   tracking pixels are not graphics votes.

Original requires main layout **plus at least one independent family**, with no
uncertainty. Fully evidenced, non-overlapping regions must cover half the retained
estimated text area (words weighted by resolved font size squared, clamped to
8–32 pixels), not half the raw word count. This prevents modest small-print
footers from outweighing prominent main cards. Multiple complete authored regions
can qualify together. The receipt's score is not an unconditional threshold:
localization and uncertainty gates also apply.

No tag-count points, p-tag percentage guard, opaque class/ID/URL identity,
sender/service lists, semantic service categories, conversation state, fixture
IDs, labels or external corpus are classifier inputs. Inline colours and nested
or sibling data tables alone are neutral.

## Limits and evaluation

This is a conservative structural estimate, not computed browser layout. Class
stylesheets, arbitrary CSS expressions, inherited visibility overrides and
intrinsic images remain limitations. HTML cannot reliably separate authored
answers inside explicit historical containers or every unmarked signature. Very
long secondary content can still overwhelm approximate visual coverage.

Per-request input/node/cumulative-depth budgets bound analysis; exceeding them
returns uncertain/simplified without affecting access to original rendering.
Cached tree summaries avoid repeated whole-subtree feature scans. The header says
“analysis uncertain” rather than claiming every uncertainty is quoted history.

The 119 anonymised fixtures contain no stylesheet bodies; most expected labels
are provisional reviewer applications of the owner's policy. Repeated templates
mean this is not unseen-family or mailbox-wide accuracy validation. The normal corpus suite runs all 119 mutation checks and 117 expected-mode
assertions. On 2026-10-22 the owner explicitly paused just the two disputed
expected-mode assertions for deployment; their desired labels remain unchanged.
Restore all 119 assertions with:

```sh
nix develop -c env CHAINMAIL_STRICT_ORIGINAL_CORPUS=1 go test ./internal/spec -run TestOriginalAnalysisAnonymisedCorpus -count=1
```

Latest targeted result: **117/119 matches, zero false positives, two false
negatives**. `thread-041-1` and `thread-059-1` are nearly identical bounded prose
alerts with a logo/bold field labels but no other recognized primary design
family. Their provisional expected-original labels remain unchanged. The explicit
strict command still fails on these two cases, while normal checks visibly
report the two temporary pauses. Revisit and restore them under
[zpm/chainmail#339](https://github.com/zachpmanson/chainmail/issues/339); do not
interpret the pause as a label correction or a classifier exception.

All 119 fixtures preserve receipts under opaque identity renaming, neutral
wrapping and added marked history/signatures. A private read-only comparison also
confirmed matching endpoint-HTML-versus-anonymised decisions for all 119 inputs;
these fixtures were collected from the sanitized `/original` endpoint, not raw
stored `body_html`. That comparison does not establish raw-analyser-input parity.
No private originals or mapping are committed. Synthetic regressions include the
three initial stress cases and the independent review's visibility, localization,
flow, contact, column and resource-limit counterexamples. The original endpoint,
manual overrides and unavailable/stale receipt behavior have separate tests.

Two additional owner-confirmed examples now pass through the generic fluid-gutter
and inline-action rules, with receipts 3 and 2. The 119 existing corpus receipts
remain unchanged. Their sanitized-endpoint provenance, exact added gates and
synthetic counterexamples are recorded in
[testdata/original-owner-examples/RULE-PROPOSALS.md](testdata/original-owner-examples/RULE-PROPOSALS.md).
The simplified sanitizer also preserves safe `picture` fallback descendants while
removing the wrapper, `source`/`srcset`, handlers and unsupported URLs; the original
renderer is unchanged.
