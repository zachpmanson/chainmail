# Hassles

Hassles is an explicitly enabled CalDAV reminder overlay. Start the server with
`-hassles` (Nix: `services.chainmail.enableHassles = true`) only when Calendar
reads/writes are intended. The default is off;
mail grants do not enable it. No service/host configuration is changed by this feature.
The existing Google connection must actually have Calendar permission; reconnect
if Calendar access is denied. Discovery uses Google’s documented account-email
`/caldav/v2/{email}/user` principal; an account with missing email metadata must
be reconnected rather than guessed. Calendar discovery does **not** prove write permission.

In **Settings → Hassles**, select one connected account and a **dedicated reminders calendar**, acknowledging
that all its VEVENT resources are tasks. No primary-calendar default, all-calendar
scan, or account fallback is used. Only that atomic location preference is saved
in corpus settings. Changing selection never moves/deletes existing events.
Task content, recurrence and notification state remain exclusively in Calendar.

## Behavior and limits

- All raw VEVENT masters are read without a time window, including past one-offs,
  exhausted series and distant reminders. No occurrence duplication or background
  scheduler. Canceled events are hidden, retained as members of an otherwise-active
  logical task where appropriate.
- The Add form stays above accessible **Upcoming / Past** tabs; switching tabs or
  editing/removing another task preserves its draft and retry UUID. Only a successful
  create or a selected-location change resets it. Edit and removal still use dialogs.
- Upcoming shows groups with future reminders, including recurring series whose first
  reminder was long ago. Past is explicitly the **last 30 days** of ended reminders,
  not completion history. Each group appears once, ordered by earliest next reminder
  (Upcoming) or most recent last reminder (Past), under Melbourne-date day headers.
  Unsupported/incomplete schedules stay visible in Upcoming, undated when no next
  occurrence is known; they are never inferred to be ended from their original start.
- One `[hassle:v1 task=<UUID>]` description marker groups resources within the selected
  calendar. Untagged/ambiguous resources stand alone, never grouped by title. UUIDs
  do not grant ownership: every master must have docket's `[docket]` marker.
  Meetings with attendees/organizers and resources without a strong ETag are read-only.
- Create uses a stable per-form UUID, a conditional resource create, five minutes,
  transparent/free, no attendees, and a DISPLAY alarm at the start. No phone delivery
  or Google behavior has been live-tested. Calendar/phone notification settings still
  determine delivery.
- Melbourne wall time is explicit in the form, transport and TZID. Default is the
  next 08:30 there, **daily until Done**, independent of browser timezone.
  Defaults apply only to new drafts, never to existing Calendar events. DST gaps **and ambiguous overlaps**
  are rejected. Supported input dates: 1970–2100; intervals: 1–999.
- Once, daily, weekdays, weekly, monthly and yearly are supported. Weekdays means
  Monday–Friday every week (interval 1); the first reminder must be a weekday. Monthly/yearly skip nonexistent dates.
  An optional inclusive repeat-end timestamp must be after the first reminder;
  the Repeat until dropdown chooses Done/no end or Date and time. **Hourly is visibly unavailable and rejected.**
- Schedule editing is deliberately limited to one simple five-minute event/series
  in Australia/Melbourne with minute precision and a representable RRULE. Imported
  exceptions, RDATE/EXDATE/COUNT or other complex rules, all-day/floating/other-zone
  events, embedded timezone definitions and multi-resource groups remain visible,
  with edit restrictions explained. Unknown calendar/event fields, human description,
  markers and alarms are preserved on supported edits.
- Read-only timing is independent of edit restrictions, including embedded VTIMEZONE.
  Known IANA TZIDs, daily/weekly/monthly/yearly RRULEs (including COUNT/UNTIL,
  INTERVAL, WKST and weekly bare-weekday BYDAY), RDATE and EXDATE are read.
  Other BY* filters are classified unknown before iteration: impossible combinations
  can make the recurrence library scan to year 9999 inside one iterator call.
  Detached exceptions, floating/all-day/unknown-zone times, high-frequency or exotic
  rules and scans beyond 10,000 generated occurrences remain honestly unavailable.
  Recurrence dates are limited to 10,000 per property family.
  The API exposes `timing.state` (`upcoming`, `past`, `unknown`), nullable RFC3339
  `timing.next`/`timing.last`, and `timing.note`. A multi-resource group uses the
  earliest known future occurrence; without a future occurrence it is Past only
  when every active resource is understood. A note flags incomplete timing, so
  a known occurrence is not mistaken for a complete schedule.
  Resource inventory is bounded to 2,000 resources, 1 MiB
  parsed resource data and 16 MiB responses; requests have bounded timeouts.
- Done and Delete remove **all** members permanently; there is no completion history.
  Group revisions and server-side rereads detect stale membership/data; PUT/DELETE
  use each real returned href and ETag. Multi-resource deletion is not atomic: a
  failure states the number removed, refreshes inventory, and never recreates
  removed resources. Retry only after checking the refreshed result.
- The token source rereads disk for each request; redirects and foreign origins
  are refused before OAuth. Discovery is revalidated for the selected account on
  each inventory/write. Client requests cannot supply arbitrary resource hrefs.

## Offline validation (no credentials or upstream fallback)

Automated flow, real typed fetch/query layer and Base UI dialogs:

```sh
nix develop --command bash -c 'npm test -- --run test/hasslesForms.test.ts test/hasslesGroups.test.ts test/hasslesFlow.test.ts'
```

Browser smoke harness, using only an in-memory **test fixture**, not the real server:

```sh
nix develop --command bash -c 'npm run build && npx --no-install tsx scripts/hassles-smoke.ts'
```

Open <http://127.0.0.1:8877/hassles>. Follow its Settings link. In the Hassles
settings section select Offline account → Offline reminders → acknowledge dedicated
overlay → Use calendar, then return to Hassles using navigation. Add a title/date, repeat interval
and end; save; inspect chronological day groups; switch Upcoming/Past; edit; Done and confirm. Add another and Delete.
Check Hourly is disabled. Tab/Shift-Tab through controls; verify dialog initial
focus, Escape to cancel, focus return and trapped tab order. Repeat at 375px and
wide desktop widths: controls should wrap, labels stay visible and dialogs scroll.
These visual/responsive checks are a runnable review path, not a claim that a real
browser or phone has been manually tested.

Inject a write-permission error (then try Add/Save or Done):

```sh
curl -s http://127.0.0.1:8877/__hassles_fixture -H 'Content-Type: application/json' \
  -d '{"mode":"read-only"}'
```

Other modes: `conflict`, `disconnected`, `disabled`, `normal`, `reset`. Refresh reminders
for read errors; reload for changed capability/account state. Restore `normal` and
retry the **same open create form** to verify its UUID is retained. The fixture
intentionally implements only the Hassles flow; other mail sections have no fallback.
Stop it with Ctrl-C. Do not use this fixture as a service.

## Dependencies / Nix

Promotes only docket's already-pinned go-ical, go-webdav and rrule-go versions.
No dependency upgrades, OAuth reimplementation or security-pin changes.
`vendorHash` is updated for the newly reachable packages. It was derived using
`go mod vendor` + `nix hash path`; the same procedure reproduced the previous
baseline hash exactly. No deployment or host/pin changes were performed.
