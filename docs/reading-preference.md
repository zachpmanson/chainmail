# Reset a sender's reading preference

`POST /v1/people/{personId}` supports returning one person's mail to automatic
rendering:

```json
{ "resetOriginalPreference": true }
```

This clears both `prefer_original` and `prefer_original_set` for that person,
without changing their name, identities or anyone else's preference. It is
idempotent and uses the existing edit transaction and server access boundary.
No mailbox operation, detector change or schema migration is involved.

- `preferOriginal: true` still forces original; `preferOriginal: false` still
  forces simplified. Omitting it or sending `null` leaves it unchanged.
- `resetOriginalPreference` absent, `null` or `false` does nothing. A request
  containing no effective edit returns 400; reset `true` is an edit by itself.
- Reset `true` together with explicit `preferOriginal: true` **or** `false`
  returns 400 before any part of the edit, including a rename or identity change.
- Reset can be combined with the other editor fields in one atomic edit.
- The person ID must be positive. A missing person returns 404. Unknown fields
  and wrong JSON types return 400, as for existing edits.

The response is the existing `PersonResponse`; it need not expose the set bit.
Verify using `GET /v1/entries/{extId}` for an entry sent by that person:
`preferOriginal` and `preferOriginalSet` are both false (or omitted, which means
false). Automatic rendering may still select original based on the existing
body heuristic; resetting is not another forced choice.

## Message control

The message header's compact reading-style select offers **Default**, **Original**
and **Simplified**. It displays the saved preference, not the resulting rendering:
Default may display original HTML when automatic analysis chooses it.

For a resolved sender the server preference (including Default) wins over stale
browser fallback. Original/Simplified save explicit true/false; Default uses the
reset request above and removes that sender's local fallback after success.
Standalone pages and unresolved/recovered entries keep the same three choices
in local storage; Default removes the local override. Other senders are left alone.

While saving, the controls are disabled. Failed saves restore the previous mode
and report an error without discarding its fallback. Reading style remains
separate from thread layout, read status, mailbox actions and app undo.
