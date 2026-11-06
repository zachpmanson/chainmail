# Narrow compose-draft preservation

The existing top-level `text/plain` save path is unchanged. A separate raw-span
path supports only a flat `multipart/alternative` with exactly one UTF-8
`text/plain` quoted-printable part and one UTF-8 `text/html` 7bit part (either
order). Each part has exactly those two MIME headers. No disposition,
attachments, related/signed/nested/extra parts, other charsets/encodings or extra
parameters are accepted. Duplicate fields/parameters, malformed/truncated
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
`dir=ltr` (unquoted, single-quoted or double-quoted), and an exact closing
`</div>`. Its raw opening/closing wrapper survives saves byte-for-byte.

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

## Byte invariants and limits

Unchanged supported alternatives retain their original body bytes and, when
editor fields are unchanged, the entire raw message. Changed To/Cc/Subject
fields use only the existing editor constructor. All other root/part headers,
root wrapper, boundary lines, separators, preamble, epilogue and metadata stay
byte-for-byte. The final guard reparses the **actual outgoing bytes**, proves
both representations equivalent, compares raw immutable skeletons and wrapper,
and runs after metadata/parent construction before any PUT. Unsupported cases
refuse on unchanged as well as edited saves.

Bounds: 2 MiB raw MIME, 256 KiB decoded text/HTML, 20,000 legacy HTML tokens,
76-byte quoted-printable lines, 998-byte HTML 7bit lines, 70-byte conservative
ASCII boundary grammar. Strict CRLF framing is required. These are parser
bounds, not HTTP transport response bounds; the Google SDK first materializes
its response.

## GET-only full preparation check

`GET /v1/drafts/{id}/save-check?accountId=…` requires explicit account selection
and no write grant. It fetches the selected draft, rejects any reply-header
context needing parent evidence, constructs unchanged and representative
Unicode/entities/repeated-space/tab/NBSP/CRLF/blank-line replacements through the
same constructor, metadata/sender checks, splice and final guard as saving, and
returns only `unchangedCompatible`, `bodyEditCompatible` and the fixed `context`
category (`compose`, `reply_context`, `unsupported`). Refusals contain no
content/IDs/preparation errors. Provider/account failures use fixed safe errors.
It never saves, sends, deletes or fetches a reply parent. Mailbox initialization
may load its normal label cache; sender verification performs profile/settings
GETs. Ordinary reads do not gain these checks.

A positive result proves **only this snapshot and the representative edit**.
It is not proof for every edit (e.g. a later boundary collision/size limit),
future revision/settings state, provider send behavior or write permission.
No live mailbox evidence was used to develop or test this path; tests use
invented Gmail-style messages and synthetic transports only.
