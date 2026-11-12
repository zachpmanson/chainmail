# Narrow mention/plaintext twin fallback

Normal twin identity and rendering remain unchanged. If the usual identity
comparison refuses, one mailbox copy and one recovered quote may match by exact
own plaintext, normalized only for whitespace, when they share a nonempty thread
container and the existing plausible clock-offset gate passes. At least 25 prose
tokens are required after the existing address/link/image residue removal; email
expansions cannot manufacture that floor. Author grouping, unique-base ambiguity,
multiple-mailbox group refusal, dropped attachments and original annotation/link
evidence remain authoritative. This is not a global email-address stripper or a
lower matching threshold.

The mailbox receives this evidence only after a positive HTML/plain proof:

- UTF-8 plaintext and HTML, each at most 2 MiB;
- balanced fragment markup containing only own `div`, `br` and text, plus anchors
  labeled with `@` and a single simple `mailto:` address;
- each anchor projects as its visible label followed by `<address>`, and the
  complete projection must exactly equal the mailbox's own plaintext;
- no query, fragment, percent-escaped/multiple address, general link, own image,
  script, style, comment, hidden/event/style attribute, unsupported envelope/tag,
  unquoted attribute value, duplicate attribute or malformed own structure;
- only an explicit `div` whose HTML ASCII-whitespace-separated class tokens
  include `gmail_quote` may be skipped as quoted history. NBSP, vertical tab and
  other Unicode spaces do not create class tokens;
- the strict raw-token projection must also agree with the actual parsed HTML
  tree's own-content projection. Tree-builder context in history (for example
  select, table or foreign content) cannot make a tokenizer depth counter omit
  an image, prose or unsupported element outside the genuine history subtree.
  Ordinary rich history/images remain untouched in storage; arbitrary
  blockquotes are not skipped and parser recovery never widens own grammar;
- at least one mention expansion must be proven. Recovered copies with stored
  HTML do not gain this fallback.

Unknown shapes deliberately remain duplicates. Original HTML-derived prose and
URL evidence is not replaced with plaintext for annotation or link decisions.
Both ingest lookup and repair use the same evidence and identity predicate;
repair still prefers the mailbox, transfers sightings/reply edges, and preserves
its original body bytes. The fallback itself never writes or rewrites content.

Reduction cache format 3 includes the exact plaintext evidence in bounded,
checksummed length framing. Older/malformed cache rows recompute; future rows are
not downgraded. Reconciliation rules version 6 makes a version-5 clean checkpoint
require one normal full verification round after a separately authorized rollout.
No database migration, forced repair, special message/person rule or deployment
is part of this source change. Tests use invented content and temporary stores.
