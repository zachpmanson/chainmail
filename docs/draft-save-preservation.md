# Narrow compose-draft preservation

The existing top-level `text/plain` save path is unchanged. A separate raw-span
path supports only a flat `multipart/alternative` with exactly one UTF-8
`text/plain` quoted-printable part and one UTF-8 `text/html` 7bit part (either
order). Each part has exactly those two MIME headers. No disposition,
attachments, related/signed/nested/extra parts or other charsets/encodings are
accepted. HTML requires only `charset=UTF-8`; plain allows only `charset=UTF-8`
with optional `format=flowed` or `format=fixed` (case-insensitive, quoted or
unquoted). No `delsp` (even `no`) or other parameters are supported. Duplicate
fields/parameters, RFC2231 extended/continued parameters, malformed/truncated
framing, invalid quoted-printable, boundary-looking collisions and unknown root
headers fail closed. Root transfer encoding is not supported on this path.

Root non-editor headers remain limited to the existing metadata allowlist
(Date, Message-ID, Received, verified raw From), MIME-Version, Content-Type and
parent-proven reply headers. This is not a general header copier. Existing
sender/profile/primary-default identity, revision, deletion, reply-parent and
thread checks still apply. From is verified even for unchanged saves.

## Positive HTML proof

This is a recognizer, **not a sanitizer**, and does not use the MIME diagnostic's
feature flags. Initial HTML must have one exact lowercase `div` root with only
`dir=ltr` or `dir=auto` (unquoted, single-quoted or double-quoted), and an exact
closing `</div>`. It may additionally have exactly `<html><body>` before that root
and `</body></html>` after it, without attributes or extra envelope markup. The
entire raw envelope, root direction and opening/closing wrapper survive saves
byte-for-byte; neither direction nor outer markup is normalized.

The legacy inner grammar is either inline text with internal `br` breaks, or
an optional initial inline run followed by one or more flat `div`/`p` blocks.
Blocks contain only text and internal `br` breaks; a block containing only one
`br` denotes an empty line. Accepted spellings are `<br>`, `<br/>`, `<br />`.
Nested blocks, empty blocks, leading/trailing breaks (except that empty-line
placeholder), inter-block text, all attributes on layout elements, other
spans/elements, comments, declarations, malformed/misnested tags, formatting,
links/images, signatures and quotes are refused. No structural browser error
recovery is used. Entity spans must end in a semicolon and change under Go's
browser-compatible HTML entity decoding, which can include legacy entity-prefix
recovery. This is not a complete named-entity grammar validator; the resulting
visible text must still satisfy the full plain/HTML equivalence check below.

Legacy projection collapses ASCII HTML whitespace within text runs, trims ASCII
spaces at visual line edges, maps `br` to LF, and separates block runs with one
LF. It must equal the decoded old plain text after CRLF-to-LF normalization.
Only two bounded legacy concessions exist:

* HTML NBSP can map to ASCII space **only if the old plain text contains no
  NBSP**, and the resulting complete strings match.
* Plain may have exactly one additional terminal transport LF, only if the
  HTML projection does not already end in LF. No arbitrary trimming of blank
  lines or other whitespace occurs.

A supported edit replaces both body spans. The generated inner representation
is exactly `<span style="white-space: pre-wrap">…escaped text…</span>`, inside
that retained root wrapper. This **single exact style/attribute spelling** is
also recognized on repeat saves; no other style is supported. It preserves
spaces, tabs, NBSP and line breaks without conflating their characters.
Generated text equivalence is exact after CRLF-to-LF normalization, without the
legacy concessions. Ampersands/angle brackets are escaped; non-ASCII characters
are numeric entities so the original 7bit CTE is preserved. Plain retains its
quoted-printable CTE. Unsafe controls and invalid UTF-8 are refused. Changed
HTML exceeding the 7bit line limit is refused rather than changing encoding.

## Narrow flowed-to-fixed normalization

This is not a flowed codec. Legacy plain `format=flowed` is supported only when
**every decoded line** has no leading ASCII space, no leading `>`, and no trailing
ASCII space (including `-- ` signature separators and soft wraps). Bare CR and
unsafe text remain refused. With no `delsp` or additional parameters, this proves
flowed semantics equal the stored plain text exactly. The complete existing HTML
equivalence proof above is still required. A failing flowed proof refuses both
unchanged and edited saves, with no writes.

On an actual body-text change from that proven snapshot, the owner-approved
transport exception changes only the raw plain Content-Type **format value**
from flowed to `fixed`, while synchronizing both bodies. Parameter key spelling,
quotes, folding, charset, CTE and every other part-header byte are retained.
This permits new literal indentation, quotes, trailing/repeated spaces, tabs,
Unicode and NBSP under fixed semantics, subject to the existing text/encoding
bounds. Plain without a format is unchanged; existing fixed plain is subject to
the same HTML/text proof but not the flowed line restriction. No-op/header-only
edits retain the original flowed header and bodies byte-for-byte.

## Byte invariants and limits

Unchanged supported alternatives retain their original body bytes and, when
editor fields are unchanged, the entire raw message. Changed To/Cc/Subject
fields use only the existing editor constructor. Except for the controlled
flowed-to-fixed format-value token on a body edit, all other root/part headers,
root wrapper, boundary lines, separators, preamble, epilogue and metadata stay
byte-for-byte. The final guard reparses the **actual outgoing bytes**, proves
both representations equivalent, compares raw immutable skeletons and wrapper,
and runs after metadata/parent construction before any PUT. The skeleton masks
only the two body slots and that positively located format-value span. An explicit
transition proof requires identical raw token bytes when format is unchanged,
and permits only proven old flowed to new fixed on a body-text change; it rejects
reverse transitions, adding/removing format, normalization without a body edit,
and flowed body edits without normalization. Unsupported cases
refuse on unchanged as well as edited saves.

Bounds: 2 MiB raw MIME, 256 KiB decoded text/HTML, 20,000 legacy HTML tokens,
76-byte quoted-printable lines, 998-byte HTML 7bit lines, 70-byte conservative
ASCII boundary grammar. Strict CRLF framing is required. These are parser
bounds, not HTTP transport response bounds; the Google SDK first materializes
its response.

## GET-only full preparation check

`GET /v1/drafts/{id}/save-check?accountId=…&expectedMessageId=…` requires
explicit account selection and no write grant. The nonempty revision witness
comes from the opened draft detail. The route reads that revision, resolves any
strict reply-header evidence through the account-qualified corpus and selected
account's parent copy, and uses the same read-only Reply audience/subject plan as
save (with explicit current To/Cc, including empty Cc). The checker re-GETs and
compares the witness before constructing unchanged and representative
Unicode/entities/repeated-space/tab/NBSP/CRLF/blank-line replacements through the
actual update constructor, real parent metadata/thread/header reconstruction,
metadata/sender checks, splice and final outgoing-byte guard. Parent headers are
never copied from the draft. Provider evidence is not cached. Updates additionally
require one valid, unambiguous live parent Message-ID and, when present, one
valid References list; duplicate/malformed evidence is refused rather than
collapsed to the last value. Original parents may omit References. Missing
Message-ID/thread evidence is indeterminate in preflight and refuses updates.
First-create and send behavior are unchanged.

Witness-bound responses always include `status`: `supported`, `unsupported`,
`indeterminate` or `revision_changed`. Only `supported`, both compatibility
booleans true, and `context=compose` or `context=reply` is positive evidence.
Every other status has both booleans false. Missing/ambiguous/unresolved parents,
missing account copies and unavailable/unknown sender or parent evidence fail
closed as indeterminate, not a definitive formatting refusal. Revision drift
returns a fixed `revision_changed` response. Actual missing draft reads are 404;
missing parents do not claim the draft disappeared. Responses contain no
content, IDs or preparation/provider error details. Explicit empty or repeated
witness parameters are 400. Omitting the witness retains the legacy conservative
compose-only check: `reply_context,false,false` is never support evidence.

The operation never saves, sends or deletes and works with both write/send flags
disabled. Mailbox initialization may load its normal label cache; reply planning
and parent/sender verification perform additional GETs. Ordinary reads do not
gain these checks. The SDK materializes responses before parser limits; the
initial detail read also uses the existing draft reader.

Legacy preparation refusals also emit a closed application check code to the service
journal (for example `alternative_plain_lines` or `alternative_framing`). These
entries contain no draft/account identifiers, error text, headers or content;
unrecognized errors map to fixed fallback codes. They do not change acceptance.

A positive result proves **only this snapshot and the representative edit**.
It is not proof for every edit (e.g. a later boundary collision/size limit),
future revision/settings state, provider send behavior or write permission.
Actual SaveDraft independently fresh-GETs and compares its required revision,
then runs all parent/sender/MIME/final-byte guards before PUT. Preflight is never
future save authorization; concurrent provider changes after a check remain
possible.
No live mailbox writes are used to develop or test this path; permanent tests
use invented Gmail-style messages and synthetic transports. An owner-authorized
read-only structural inspection informed the exact envelope/direction extension
and the narrow owner-approved flowed-to-fixed exception; no private message
content is retained in repository fixtures.
