# Undo

Use **Ctrl+Z** (or **⌘Z** on macOS), or the Undo arrow in the navigation bar.
Undo applies application actions in reverse order, across folder/account changes.
Inputs, textareas, selects, contenteditable regions and textbox widgets keep their
native editing undo. Shift+Ctrl/⌘Z is not intercepted; application redo is not provided.

The history is session-local, limited to 100 actions, and cleared on page reload.
Forward mailbox writes and undo share a queue: pressing undo during a pending write
waits for that write; later writes cannot overtake the reversal. Failed undo stays
on top for retry. A toast reports failures; it never claims an unsuccessful undo worked.
Reload to abandon history if an expired or conflicting top entry cannot be restored.

## Supported actions

- **Mark read/unread:** restore each original message's own UNREAD state, including
  mixed read/unread chains.
- **Archive:** restore previous INBOX membership, not blindly move everything to Inbox.
- **Trash:** restore prior INBOX/TRASH membership. This is recoverable trash, not
  permanent deletion; Gmail expiry or deletion elsewhere can make undo impossible.
- **Move:** restore membership in INBOX and the named destination labels. Destination
  labels that were already present remain; unrelated labels are preserved.
- **Compact layout and reply-tree layout:** restore the previous browser preference.

Mailbox operations snapshot fresh labels before writing, rather than relying on the
possibly stale corpus. Receipts bind to exact original account/message IDs. New replies
arriving in a chain and copies in other accounts are not part of the reversal. No-op
writes do not consume history. Overlapping selected chains have one reversal per copy.

Partial forward failures expose undo for **confirmed** writes (including a mailbox write
whose mirror update failed). A lost forward HTTP response can lose its receipt; check
Gmail rather than assuming nothing changed. The failed mailbox request itself may have an unknown
outcome: check Gmail; it is not falsely included as a confirmed success. Undo failures
retain only unfinished messages for retry, and retries are idempotent even if a successful
HTTP response was lost. Read-state undo still requires `-mark-read`; other mailbox undo
requires `-mail-write`. Disconnected accounts, expired receipts (server restart or 100-receipt
eviction), deleted messages/labels and revoked grants are explicit failures.

Before undo, the server checks every touched label (including labels absent in both
states) and refuses unexpected changes from other clients. Per-label revisions also
reject out-of-order overlapping writes from other tabs; undoing the newer receipt
restores the older receipt's eligibility. Unrecorded/no-op/uncertain later writes
conservatively block older overlapping receipts. Unrelated label changes survive. **Gmail has no atomic compare-and-swap:**
an external client can still race between snapshot/preflight and modify. Neither this
history nor the server's serialization locks out Gmail, cron jobs or other mail clients.

## Not in application undo

- **Send/reply/send draft:** mail already sent cannot be recalled.
- **Draft creation/autosave/deletion:** Gmail owns drafts; delete/recreate is not an
  identity-preserving restore and autosaves race other clients. Text editing retains
  native undo; there is no promise of reversing Gmail draft lifecycle changes.
- **Identity edits/merges, org rules, server settings/default folder:** the existing
  APIs lack a lossless journal/conditional restore. In particular unset reading-style
  overrides, legacy reader identity and implicit cadence differ from explicit values.
  Replaying visible values could silently overwrite newer or inherited configuration.
- **Sign-in/disconnect:** credential restoration requires Google authorization.
- **Ingest/refresh, attachment pulls, saved page builds:** corpus/filesystem jobs are
  not transactional reversals and may incorporate new external data.
- **Navigation, selection, pane dragging, message disclosures and sender-style fallback:**
  transient/view state is not application history. Browser Back handles URL navigation;
  sender style can also be a server-backed person override and must not be half-undone.

## API

`POST /v1/read` and `POST /v1/mail` accept `undoable: true`. Capture failure refuses
before any write. An optional `undoToken` in success/error JSON is a process-local,
opaque receipt. `POST /v1/mail/undo` accepts only `{ "token": "…" }`; callers cannot
supply substitute account/message IDs or label snapshots. Original write grants still
apply. Completed receipts are retained until eviction so response-loss retries are safe.
