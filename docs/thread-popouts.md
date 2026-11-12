# Managed thread pop-outs

**Open in new window** moves the reader and reply editor out of the initiating
app tree. The main window shows an away placeholder with List/Results, Focus,
and recovery controls. Reader-only threads also move. Compose has no pop-out
control.

A reply move requires initialized blank/default input or an exact acknowledged
saved reply. Pending recipient text (not yet accepted as a chip), unsaved or
pending edits, stopped saves, review, uncertain scope/selection, and nondefault
reply-all/HTML options remain in the current window. Expanding the recipient
fields alone does not prevent a move. Reader-only eligibility requires a loaded,
verified chain with no reply editor and no pending draft selection; temporary
loading or error states are not reader-only evidence. Unscoped Search and All mail
remain unscoped during reader-only moves and Return; no mailbox is inferred.

## Ownership and revision handoff

A random one-attempt token names a same-origin BroadcastChannel. The listener is
installed before `window.open`, which keeps `noopener`. A null open result is
not treated as a blocked popup. The destination starts inert, independently
checks its chain, and checks any saved draft against the source's acknowledged
**message revision**, account, ID and exact reply target. It retains that checked
snapshot in destination memory for initial admission; a later same-ID revision
cannot silently replace the acknowledged body during handoff. Ordinary fresh
preflight and save guards still stop later drift.

The source freezes input while preparing, rechecks its live editor/revision,
invalidates and synchronously unmounts it, then grants ownership. Preparation
failure or timeout before release retains the source. Missing confirmation
*after* release never automatically reactivates it.

An account+root-qualified away record survives source navigation. Reopening that
thread shows the placeholder, not a second editor. Managed identity is independent
of mutable route flags. Same-thread draft-card selection remains managed, including
selection from a blank popup; ordinary routing cannot change its account, root,
token or host into a standalone editor.

## Return and native close

**Return to main window** first asks the main window to prepare the popup's
current acknowledged state, including its current ID/revision/target (or no ID
after send/discard). The main window refuses while another editor is open or the
saved revision cannot be checked. A definite refusal leaves the popup usable,
so Return can be retried. Only a matching prepared response releases the child;
the main window then admits the checked snapshot, never the old source buffer.
Return preparation attempts are separately identified so late responses cannot
release a later attempt.

Actual native `pagehide` permanently abandons even an unfinished preparation or
undelivered commit, so late messages cannot activate a resumed page. After commit,
it synchronously quiesces the child before notifying the main window. A safe acknowledged snapshot can be freshly admitted there without
refreshing. Cancelled `beforeunload` does not release ownership. A BFCache-restored
child remains paused rather than resuming an old editor. Close uses existing
beforeunload protections; no unsaved text is transmitted or reconstructed.

If native closure has an uncertain save, another main editor blocks recovery, or
the release packet is lost, the source stays away. **Reopen saved state** is a
manual recovery action requiring confirmation that the other window really is
closed. It retains any validated current ID/target or explicit no-ID release even
when immediate admission is blocked, then checks that saved state afresh. It does
not claim to recover unsaved popup input or genuinely unknown new draft IDs. Other saved drafts remain available through
the normal draft list. Do not confirm closure while the popup is still active.

## Limits and validation

This coordinates one initiating app tree and its managed popup, not every browser
tab. Independent tabs, manual direct popup URLs and a restarted/refreshed main
page are outside the registry's lifetime. No heartbeat, timeout or null window
handle is used to infer closure. Ambiguous post-release failures stay paused
until explicit recovery. Managed popup refreshes/stale tokens fail closed;
ordinary no-token `popup=1` URLs remain standalone.

No body text crosses the channel or enters localStorage. No automatic Gmail
deletion is added. Tests use invented HTTP fixtures and actual router/editor
hosts. `scripts/thread-popout-smoke.ts` exercises the production bundle in real
Chromium pages, including recipient-query refusal, revision drift, popup saves,
Return refusal/retry, managed draft selection, narrow Inbox/search navigation,
reader-only movement, and native close/cancel. No live mailbox testing is needed.
