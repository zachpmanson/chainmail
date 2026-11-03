# Embedded SQLite security update

Tracking: [zpm/chainmail#372](https://github.com/zachpmanson/chainmail/issues/372).

Validation checkpoint: targeted fixtures and full `make check` passed with the
standalone Go 1.26.6 dev shell. The actual Naboo nixpkgs-context package also built
and passed its tests with Go 1.26.10; the vendor hash matched. All three executable
module records and exact built test sources were verified, with **13 byte-identical
frontend assets** versus the deployed UI. This is not deployment/release approval.
Chainmail embeds SQLite through `modernc.org/sqlite`; updating the host SQLite shared
library or CLI does not update the Go executables.

## Dependency boundary

- `modernc.org/sqlite`: v1.57.0 → v1.61.1 (SQLite 3.53.3 → 3.54.1).
- `modernc.org/libc`: v1.74.4 → v1.79.0, matching the mandatory upstream pin.
- Required MVS changes: `modernc.org/memory` v1.11.0 → v1.12.1,
  `golang.org/x/sys` v0.47.0 → v0.49.0, `github.com/dustin/go-humanize`
  v1.0.1 → v1.1.0, `github.com/ncruces/go-strftime` v1.0.0 → v1.1.0.
- Tidy also updates dependency/test-tool checksums for pprof, x/mod, x/sync,
  x/tools, cc/v4, ccgo/v4 and gc/v3; these are upstream graph requirements,
  not new direct application dependencies.
- Preserve `go 1.26.4`, flake.lock, docket and all unrelated pins. The pinned Nix
  dev shell currently supplies Go 1.26.6; no toolchain change was made here.
- No production Go/SQL, schema, DSN, UI, rendering, draft or cache changes.
  No new defensive, authorizer, OFD, VFS or other opt-in settings are enabled.

The [tagged changelog](https://gitlab.com/cznic/sqlite/-/blob/v1.61.1/CHANGELOG.md)
and tagged go.mod require Go 1.26 and libc v1.79.0. v1.61.1 fixes v1.61.0's
prepared-statement replan regression; it also changes cancellation/lock handling
and fixes read-only VFS behavior. Those semantics require validation, not just a
version check. Repository Go imports use the driver package only, not `lib`,
`vfs`, `vec` or `pcache`: the nine native-header constants removed in v1.60.0
are not application imports.

`go mod verify` succeeded in a private module cache. The checksummed v1.61.1
module records libsqlite3 v1.15.2 commit
`6124339364eb60da198c6d046265cc490f07fffa` in `vendor.json` (its generation
metadata says Go 1.27.2, not the application's minimum Go version).
`lib/sqlite.go:4564` defines 3.54.1, and its source ID at line 4334 is
`2026-10-16 14:45:20 555e07ef33f69a0957d24d8e555b702464a131d6f1607f4c3c1e4551e8e6ca76`.
Compiled module metadata is now verified for server, corpus and demo-seed.
The native engine version was exercised through the new synthetic tests, not a
live corpus or application request.

## Source-level security classification

Reviewed the CNA descriptions and upstream patches, then the actual generated
Go for **linux/amd64**, including build tags. This is unsafe C-to-Go translation,
not protection supplied by Go bounds checks. The old vendored source was read
only, never built or run. Its FTS5 merge allocation lacks the extra nine bytes.

Below, `g_SUFFIX` means `lib/sqlite_g_SUFFIX.go` in modernc/sqlite v1.61.1.
"Compiled" is not a claim that Chainmail exposes the feature or exploit path.

| CVE-2026- | Feature / old-source classification | Equivalent fix in new generated source |
| --- | --- | --- |
| 119371 | Core CTE expansion compiled; old CTE resolver lacks nesting accounting. | `g_0000000000001e80:2843` increments nesting and calls the limit check in `g_0000000000003e80:2587`. |
| 119372 | FTS3/4 plus FTS3_PARENTHESIS required; not enabled/generated in either linux/amd64 module. | Not applicable to this embedded build: no `_fts3BestSnippet`; do not count as an embedded fix. |
| 119373 | FTS5 compiled; old `_fts5MergeRowidLists` allocation omits +9. | `g_000000000001fea8:4914` allocates both lists **+9**; lines 4922/4926 subtract unsigned rowids. |
| 119374 | FTS5 compiled; requires secure-delete and malformed index. | `g_000000000001fea8:3302` uses 64-bit `iKeyIn`; lines 3599–3641 retain original page bound, reject excess key offsets, use separate `nNewPg` and guard index deletion on success. |
| 119375 | Core WAL compiled; old frame read truncates without clearing the remainder. | `g_000000000001fea8:15051` zero-fills `pOut+sz` through `nOut` before truncating the read. |
| 119376 | Original COLLATE depth class; old expression wrapper omits height accounting. | `g_000000000001ffff:7620–7627` gates on no parse error, increments height and checks it. |
| 119377 | Core authorizer compiled, but Chainmail does not install an authorizer. Old lookup checks only the first expression. | `g_0000000000000480:7256–7268` loops over **every indexed** joined-column expression, including upstream's follow-up correction. |
| 119378 | R-Tree compiled; old node writer hashes last_insert_rowid without a zero check. | `lib/sqlite.go:26610–26622` returns CORRUPT_VTAB for zero node ID rather than hashing it. |
| 119379 | Session compiled; no application changeset API imports. Old apply reinitializes only when table name changes. | `g_0000000000003e80:12784–12792` uses `sessionChangesetNext(...,&bNew)` and resets per-table state for every new block, even a repeated name. |
| 119591 | Core count-of-view optimization compiled; old loop does not discard an expression after parse error. | `g_0000000000001e80:2139–2142` deletes/resets the expression on error during construction. |
| 119586 | CLI `.recover` output injection, not a bundled driver feature. | Not fixed by the version label (advisory includes 3.54.1); shell/recover entry points are absent here. Host CLI remediation is separate. |

`lib/sqlite_linux_amd64.go:1` records FTS5, SESSION and RTREE generation flags,
not FTS3/FTS4/FTS3_PARENTHESIS. Thus eight of the nine newly reported library
classes have compiled code here, plus the original 119376 class; the FTS3 class
is excluded. Reachability and impact still depend on SQL, DB bytes and settings.

Upstream patch references (GitHub mirror of SQLite):

- [CTE depth](https://github.com/sqlite/sqlite/commit/e6ff280c839fdfc7418fd5add12e8f32702a17fb)
- [FTS3 snippet](https://github.com/sqlite/sqlite/commit/a87bd2471a9c0606e78d3c3678bcd9cecaa0e731)
- [FTS5 merge](https://github.com/sqlite/sqlite/commit/e1c9934f9f11a499029abb020ea715a08970da80)
- [FTS5 delete](https://github.com/sqlite/sqlite/commit/d9de6eedbe918c074c7556400fa21df5960704d9)
- [WAL tail](https://github.com/sqlite/sqlite/commit/e5bf119821f25be1d7f10ef0a2edbe175289166e)
- [COLLATE depth](https://github.com/sqlite/sqlite/commit/d54522a5b832422ab893a1e9fa7a99ab314ea4e2)
- [Authorizer](https://github.com/sqlite/sqlite/commit/7ff247c5b9ead3ad8a69c26d3fd264878e127237)
  and [follow-up](https://github.com/sqlite/sqlite/commit/5694c3bb2a6a40770ecd2f65910eea756734dfd8)
- [R-Tree](https://github.com/sqlite/sqlite/commit/d21bd37c7cfc1b85b17a6703555f0926b6236e04)
- [Session](https://github.com/sqlite/sqlite/commit/64459c87cc702d600704fb77ee40ca1782398a52)
- [Count of view](https://github.com/sqlite/sqlite/commit/eff50a067c55f972909e7780e506e91382d08de2)

## Prepared regression and remaining validation

`TestSQLiteSecurityFTS5PrefixNegativeRowids` creates only in-memory synthetic
FTS5 tables, with private HOME/XDG directories. It checks 32 signed rowids
(including zero), eight terms sharing a prefix, all detail modes, results before
and after optimize, FTS integrity and database integrity. It first requires the
reviewed embedded 3.54.1 version. No corrupt files, exploit payloads, original
software execution or user data are involved. These cases and the compatibility/
migration fixtures passed both source validation and the package's Nix test phase.
Correct query results alone are not a memory-safety proof; the source checks above
are independent evidence.

The vendorHash was precomputed and then **verified by the actual Nix FOD/package**:
`sha256-i+HuAIRw52qInPnTZAW6VsNTTmo8/bAS6TEhzLh3wfQ=`. Precomputation used private
`go mod vendor -o <scratch>/vendor` and `nix hash path <scratch>/vendor`; the real
build matched without bypassing verification. Only the frontend, modules FOD and
application (three derivations) were built, not a dependency world.

To reproduce, use the project Nix dev shell, `GOTOOLCHAIN=local`, `GOMAXPROCS=2`,
`GOFLAGS=-p=1`, an empty private HOME/XDG, private GOMODCACHE/GOCACHE and only
temp/in-memory fixtures:

1. `go test -count=1 -timeout=3m ./internal/corpus -run '^TestSQLite'`.
2. Validate ordinary porter/trigram search and FTS update/delete/rebuild;
   prepared statements repeatedly binding 0/1, NULL, text and blobs across joins
   and ORDER BY; cancelled PrepareContext/BeginTx/queries, bounded busy waits,
   rollback and subsequent connection usability. These compatibility cases
   were added and passed. Blocked Commit/Rows.Next/Rows.Close
   cancellation paths are not specifically covered by the new blocked-write fixture.
3. Exercise fresh schema creation, existing synthetic schema migrations,
   transactions/rollback, WAL reopen/checkpoint and read-only synthetic Slack
   archives; check integrity/foreign keys on fixtures. Never open a user corpus.
4. `make check` (includes frontend build, Go tests, `npm run lint`, formatting,
   vet, gofmt and TypeScript checks). Lint must pass before any eventual push.
   Keep `CHAINMAIL_CORPUS` pointed at a private nonexistent/temp path; no corpus
   slurp/status/ingest/embed targets, real APIs, models or devices.
5. Full Naboo-context package build (do not change flake.lock):
   `nix build --no-link --max-jobs 1 --cores 2 --no-write-lock-file --override-input nixpkgs github:nixos/nixpkgs/08d3a97586f6b6343b3b5e799980773629432610 .#chainmail`.
   Confirm vendorHash and package tests,
   inspect every produced Go executable (including demo-seed) with `go version -m`, confirming SQLite
   v1.61.1/libc v1.79.0. Query `sqlite_version()` and `sqlite_source_id()` through
   the newly built embedded driver on a private fixture, not the native CLI.
6. Independent review of source evidence, dependency diff and compatibility
   results. Parent owns staging/publishing and the separate application pin;
   no activation, service operations or deployment are part of this change.
