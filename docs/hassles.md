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
The Hassles screen has no visible page heading; its main landmark remains accessibly named.
Configured calendar details and its settings link stay in Settings, not the Hassles view.

## Behavior and limits

- All raw VEVENT masters are read without a time window, including past one-offs,
  exhausted series and distant reminders. No occurrence duplication or background
  scheduler. Canceled events are hidden, retained as members of an otherwise-active
  logical task where appropriate.
- The reminders list is centred independently of the Add form, capped at 40rem
  and filling available width on narrow screens. The 450px Add form sits on its left
  only when the content container fits both plus a balancing right track and gaps
  (`calc(900px + 43rem)`); otherwise it stacks above and fills the list's width.
  The page cap includes
  its padding (`calc(900px + 45rem)`), so root-font changes preserve the same fit rule.
  Container queries use actual available
  width, not just the window breakpoint. App navigation/sidebar is unchanged. Switching tabs or
  editing/removing another task preserves its draft and retry UUID. Only its acknowledged
  create or an explicit **Start a separate new hassle** resets a submitted draft. Attempted
  creates survive in-app navigation and remounts, scoped to their original calendar;
  switching calendars never reuses an uncertain create's UUID for another calendar.
  The non-interactive task heading has its own row, with a known work-hours badge
  at the top right; without a badge the title keeps the full width. The pencil and
  both completion controls stay together beside the grey schedule text below it.
  The pencil edits its whole recurring series. Separate
  tasks with the same title are not linked. Every card has two completion buttons:
  single check **This instance only**, double check **All instances**, with task-specific
  accessible names/tooltips. Edit and all-instance removal use dialogs. The edit dialog
  owns the only panel frame/padding; shared fields do not repeat the inline Add card's
  border, and the dialog's padding stays inside its viewport-bounded width. All instances
  retains the permanent-removal warning. There is no separate Delete control.
  Repeat, Every (with the actual hour/day/week/month/year unit) and Until share one row.
  The interval control retains a readable minimum width; its unit can wrap within Every
  on narrow forms rather than hiding the selected value.
  Repeat lists Once, Hourly, Daily, Weekdays, Weekly, Monthly, then Yearly.
  The title uses a four-row textarea in both Add and Edit; line breaks become spaces
  on save so the Calendar title remains one line. The form names Melbourne under When;
  the checkbox label is simply Work hours only.
  Routine explanatory small print is omitted;
  validation errors, timing uncertainty and edit restrictions remain visible.
  Rows use the raised card surface (white in light mode, theme-aware in dark mode)
  and show a compact Melbourne date/time plus recurrence and repeat end when editable.
  A small **work** badge appears at the card's top right only when the known schedule has
  Work hours only enabled; unchecked or unknown schedules do not imply the restriction.
- First reminder offers In an hour, This afternoon (13:00), This evening (18:00),
  Tomorrow morning (08:30), and Custom. Only Custom shows the date/time picker.
  Preset labels include their Melbourne clock time in parentheses; Custom stays unadorned.
  The selected In an hour label reflects the draft's fixed start, including retries; an
  unselected In an hour preview refreshes on focus, pointer or keyboard interaction without
  changing the draft. All wall times are Melbourne; passed afternoon/evening times roll to tomorrow.
  In an hour adds 60 elapsed minutes at selection, rounded down to the minute.
  Selection fixes the payload for retries; existing schedules open as Custom.
  New hassles default to In an hour, with no custom picker shown.
  Ambiguous or nonexistent daylight-saving times still require a different choice.
- The global refresh on Hassles reloads reminder settings, then only the selected
  calendar inventory. It does not fetch mail or invalidate unrelated mail queries;
  an unchanged location preserves the Add draft and retry UUID. Once a selected location
  is known, Add stays usable during settings/inventory refreshes, including the initial
  inventory read. Cached unrelated rows remain usable during reads and other rows' writes.
  Actual writes still block Settings calendar selection and the navigation refresh control.
  Conversely, an in-flight calendar selection blocks Add and row writes even after navigating
  back to Hassles. Only the server-confirmed selection response replaces cached configuration;
  older config reads are cancelled before publication, so a delayed readback never enables
  writes against the previously selected calendar. Invalid configuration and unavailable
  initial inventory retain their safeguards. After
  a write, cancel any older exact-calendar inventory read before fetching the post-write
  list so a stale initial response cannot hide a newly created hassle.
  Reading status occupies the header's deploy-stamp slot (reminders while Hassles is
  reading, corpus for other reads); the commit/date return when idle. Create/edit/Done
  acknowledgements use five-second toasts; pending work and refresh-needed errors also
  remain visible beside the list until reconciled.
- Create, edit and both completion actions update the screen optimistically. Only the
  affected row (or the single submitted Add slot) is reserved; other tasks and Add remain
  usable. Create shows a UUID-deduplicated placeholder; edit shows the submitted title
  and schedule with provisional timing. All instances hides its row after confirmation.
  This instance only suppresses the displayed witness and shows **Updating next reminder…**,
  never a guessed successor or an assertion that the series ended. Calendar remains the
  authoritative inventory; temporary rows have no usable revision or instance actions.
- Successful writes remain **Saved; awaiting Calendar refresh…** until a successful read
  started after acknowledgement. A failed GET is not a failed save: **Retry refresh** sends
  only a GET, never another write. Concurrent and cancelled reads cannot retire newer work.
  Failed writes remove only their own optimistic projection and show the freshest cached
  row, not a whole-list rollback. A failure can mean a lost response or partial deletion;
  revision/witness-sensitive row actions wait for authoritative refresh, with no Calendar undo.
  Failed edit drafts can be explicitly restored without reopening over another editor;
  their original revision is retained, never silently upgraded to overwrite a conflict.
- Failed/uncertain creates retain the exact submitted payload and UUID in memory for the
  lifetime of the app's QueryClient, including navigation away and back. Their Add fields
  are locked; Add retries that identical request without automatically resending it.
  **Start a separate new hassle** explicitly abandons that retry slot and uses a new UUID
  (the previous create may already exist). An acknowledged create frees a fresh Add draft
  immediately, even while its row awaits readback. Browser reload/session persistence is
  not provided; no task content is written to browser storage.
- Upcoming shows groups with future reminders, including recurring series whose first
  reminder was long ago. Ended shows the **last 30 days** of ended reminders,
  not completion history. Each group appears once, ordered by earliest next reminder
  (Upcoming) or most recent last reminder (Ended), under Melbourne-date day headers.
  Unsupported/incomplete schedules stay visible in Upcoming, undated when no next
  occurrence is known; they are never inferred to be ended from their original start.
- One `[hassle:v1 task=<UUID>]` description marker groups resources within the selected
  calendar. Untagged/ambiguous resources stand alone, never grouped by title. UUIDs
  do not grant ownership: every master must have docket's `[docket]` marker.
  Meetings with attendees/organizers and resources without a strong ETag are read-only.
- Create uses a stable per-form UUID, a conditional resource create, five minutes,
  transparent/free, no attendees, and a DISPLAY alarm at the start. Calendar/phone
  notification settings still determine delivery; phone delivery is not live-verified.
- Melbourne wall time is explicit in the form, transport and TZID. New reminders start
  an hour from draft creation and repeat **daily until Done**, independent of browser timezone.
  Defaults apply only to new drafts, never to existing Calendar events. DST gaps **and ambiguous overlaps**
  are rejected. Supported input dates: 1970–2100. Non-hourly intervals are integers 1–999.
- Once, hourly, daily, weekdays, weekly, monthly and yearly are supported. Hourly Every
  offers exactly **0.5, 1, 2, 3, 4, 6, 8, 12 or 24 hours**; 0.5 means 30 minutes.
  Arbitrary intervals such as 0.6, 5, 7 and 999 hours are rejected, not approximated. Weekdays means
  Monday–Friday every week (interval 1); the first reminder must be a weekday. Monthly/yearly skip nonexistent dates.
  An optional inclusive repeat-end timestamp must be after the first reminder;
  the Until dropdown chooses Done/no end or Date and time.
- **Work hours only** is optional and defaults unchecked on new drafts, in both Add
  and Edit. Checking it does not expand an explanatory paragraph; the label and validation
  remain visible. Notification starts must fall Monday–Friday, 09:00 inclusive to 17:00
  exclusive **Australia/Melbourne**; no public-holiday logic. An out-of-hours first
  reminder is rejected with actionable validation, never silently moved (including
  presets such as Tomorrow morning at 08:30). Outside-window occurrences are skipped,
  not deferred. The original DTSTART-anchored every-N-hour cycle continues through
  nights and weekends, rather than restarting at 09:00 or counting business hours.
  For example, every 3 hours from Friday 6 November 2026 at 15:00 next notifies at
  09:00, 12:00 and 15:00 Monday. Every 0.5 hours from Friday at 16:45 next notifies
  at 09:15 Monday, not 09:00. Hourly phase is Melbourne **wall-clock** time,
  not an elapsed-hour promise across DST. Nonexistent recurrence wall times are skipped;
  a repeated recurrence wall time uses its first occurrence (RFC 5545), while ambiguous
  user-entered first/end times remain rejected.
  Monthly/yearly filtering preserves the original day/month; it never expands into
  all weekdays. A five-minute event starting at 16:59 is allowed.
  Standard RRULEs carry all recurrence behavior; `[chainmail:work-hours:v1]` in
  DESCRIPTION retains checkbox intent for once/weekly/weekdays schedules that otherwise
  have identical rules. That marker grants no ownership, and never overrides filters
  or first-reminder validation. Duplicate/unknown marker versions disable editing.
- Hourly uses a single **DAILY** RRULE, daily interval 1, exact sorted BYHOUR and
  BYMINUTE slots, optional weekday filtering and inclusive UTC UNTIL. It never emits
  native FREQ=HOURLY. Integer hour choices divide the civil day; the 0.5-hour choice
  uses two minute slots, preserving the original minute offset across midnight.
  `[chainmail:hourly:v1 hours=<choice>]` in DESCRIPTION retains logical cadence and
  interval (12/24-hour work-hours rules can otherwise look identical). The complete
  rule must match those slots; missing/duplicate/unknown/conflicting metadata is not
  trusted and never grants ownership. Title-only edits retain the original properties;
  timing edits remove only exact owned markers, preserving raw notes/escapes/parameters.
  Native HOURLY and generic imported slot rules remain unsupported.
  Prior bounded Google CalDAV testing rejected native HOURLY with COUNT=3 and accepted
  DAILY+BYHOUR+BYMINUTE at one hour (COUNT=3) and two hours (inclusive UNTIL). That proves
  storage/readback of those bounded candidates. A separately owner-authorized follow-up
  also accepted/read back three isolated test series: unbounded 0.5-hour work-hours
  (Friday 16:45 → Monday 09:15 through 16:45), unbounded 0.5-hour plain (minute slots
  17/47), and 3-hour work-hours at 09:17/12:17/15:17 with inclusive Monday 15:17 UNTIL.
  The parent verified their expected 17/16/4 client-expanded occurrences, deleted all
  three (direct 404 and empty searches), and confirmed all 58 actual reminder identities,
  revisions and writable flags unchanged. This is stored-recurrence/client-readback
  evidence, not raw-rule/metadata byte identity, server expansion, all interval choices,
  live recurrence editing, DST, Google UI or phone delivery. No native HOURLY was retried.
  The code implementation itself used only offline fixtures; further live probes need
  separate owner authorization.
- Schedule editing is deliberately limited to one simple five-minute event/series
  in Australia/Melbourne with minute precision and a representable RRULE. Imported
  exceptions, imported RDATE/EXDATE/COUNT or other complex rules, all-day/floating/other-zone
  events, unsupported embedded timezone definitions and multi-resource groups remain visible,
  with edit restrictions explained. One embedded Google Melbourne definition is permitted:
  exactly one STANDARD (first Sunday in April at 03:00, +1100 → +1000) and DAYLIGHT
  (first Sunday in October at 02:00, +1000 → +1100), with the provider's 1970 anchors.
  Only the recognized optional location/name labels are accepted; extra/duplicate properties,
  transitions, recurrence dates or rule parts fail closed. These projected rules disagree with
  historical IANA data, so both existing and requested start/end dates must be in 2008–2100.
  Native calendars without embedded definitions retain the 1970–2100 input range.
  Ongoing recurrences use the current projected rules, as native schedules do; future legal
  timezone changes may require revisiting this profile. Unknown calendar/event fields, human
  description, markers, alarms and the embedded component are preserved on supported edits.
  Title-only edits also preserve time and recurrence properties without normalization.
- Read-only timing is independent of edit restrictions, including embedded VTIMEZONE.
  Known IANA TZIDs, daily/weekly/monthly/yearly RRULEs (including COUNT/UNTIL,
  INTERVAL, WKST and weekly bare-weekday BYDAY), RDATE and EXDATE are read.
  Other BY* filters are classified unknown before iteration: impossible combinations
  can make the recurrence library scan to year 9999 inside one iterator call.
  Detached exceptions, floating/all-day/unknown-zone times, high-frequency or exotic
  rules and generic scans beyond 10,000 generated occurrences remain honestly unavailable.
  Narrow uncounted hourly/work-hours profiles use bounded civil-time evaluation with
  a seek near the current time, preserving DTSTART phase even after more than 10,000
  historical hours. Each direction examines at most 10,000 candidates, including skipped
  dates and exclusions; budget exhaustion is unknown, not proof a series ended.
  Generic imported/COUNT/DATE recurrence admission is unchanged; unreachable imported
  hourly filters never enter the recurrence library's potentially unbounded iterator.
  Recurrence dates are limited to 10,000 per property family.
  The API exposes `timing.state` (`upcoming`, `past`, `unknown`), nullable RFC3339
  `timing.next`/`timing.last`, and `timing.note`. A multi-resource group uses the
  earliest known future occurrence; without a future occurrence it is Past only
  when every active resource is understood. A note flags incomplete timing, so
  a known occurrence is not mistaken for a complete schedule.
  Resource inventory is bounded to 2,000 resources, 1 MiB
  parsed resource data and 16 MiB responses; requests have bounded timeouts.
- Done removes **all** members permanently; there is no completion history.
  The API retains its equivalent `delete` action for compatibility, but the UI offers only Done.
  Group revisions and server-side rereads detect stale membership/data; PUT/DELETE
  use each real returned href and ETag. Multi-resource deletion is not atomic: a
  failure states the number removed, refreshes inventory, and never recreates
  removed resources. Retry only after checking the refreshed result.
- The token source rereads disk for each request; redirects and foreign origins
  are refused before OAuth. Discovery is revalidated for the selected account on
  each inventory/write. Client requests cannot supply arbitrary resource hrefs.

## Metadata-only legacy adoption (API/operator workflow)

`POST /v1/hassles` also supports `action: "adopt"`; there is no adoption UI.
It appends only the canonical `[hassle:v1 task=<UUID>]` marker to every VEVENT
DESCRIPTION in **one existing resource**, including detached/canceled exceptions.
Missing exception descriptions are created. Existing descriptions (including their
parameters and original escapes), human notes, UID, schedule, alarms, unknown
properties and embedded VTIMEZONE are preserved. VALARM descriptions are not tagged.
No timestamp is intentionally changed, no resource is created/replaced/moved, and
same-title reminders are never grouped by adoption.

The existing `-hassles`, connected account, selected/discovered dedicated calendar,
selection mutex, real resource path, strong ETag, docket ownership and participant
guards still apply. A UUID marker never grants ownership. Skip protected/read-only
rows even if an operator has manually added a marker; do not override an upstream
`NOT_OWNED` result or try to reach an inventory count. Inventory counts can change
as upstream resources change. Schedule-editing restrictions are independent:
otherwise-owned imported exceptions/timezones can receive metadata without schedule
editing. Wholly canceled resources remain hidden and are not adoption candidates.

1. Read the selected calendar inventory and choose the original opaque
   `resource:<path-digest>` ID and its exact `revision`. Generate and retain a
   stable client UUID for this resource. Never choose by title.
2. Preview with `{location, action: "adopt", id, revision, adoptId, dryRun: true}`.
   No write occurs. The `adoption` result returns `resourceId`, `revision`, new
   `taskId`, `fingerprint`, and `before`/`after` iCalendar exports. Reject/review
   unexpected differences before committing.
3. **Save the preview, original identity/revision, UUID and fingerprint locally**
   before any commit. These exports are semantic reserializations of parsed
   calendars, **not byte-exact HTTP backups**: ordering/folding/escaping may differ.
   Keep the original export separately; never overwrite it with retry output.
4. Commit the same request with `dryRun: false` and `adoptHash` equal to the saved
   preview `fingerprint`. The server rereads the selected resource, checks the
   original revision and protected-content hash, then performs one conditional
   PUT to the existing path with its real strong ETag. A stale revision/hash or
   upstream conditional failure is a conflict; no automatic refresh-and-overwrite.
5. Reread after every actual write and compare its protected-content fingerprint
   with the saved preview. An exact retry using the **original resource ID**,
   original revision, same UUID and saved `adoptHash` returns `Already adopted`
   without a write, even though the visible task ID/revision changed. A dry-run
   retry also reports already adopted, not a new original backup. Changed protected
   content, partial/mixed tagging, moved paths, lost ownership or collisions fail
   closed. Retry output exports the current tagged resource in both before/after.

The fingerprint retains all parsed content except the exact appended marker and
provider-maintained VEVENT `DTSTAMP`, `LAST-MODIFIED`, and `SEQUENCE` properties.
Preparation additionally checks preservation of these revision fields; the exception
only accommodates provider changes on reread/retry. Other provider normalization
may cause a conservative conflict and requires review, never blind overwrite.
Raw resources (including canceled tasks and malformed/duplicate descriptions) are
scanned for UUID reuse and duplicate UID claims. Pre-existing marker prefixes are
conservatively refused, including case variants, malformed/repeated markers and
unknown versions. Existing tagged resources are not adoption candidates except an
exact completed retry. Ambiguous UID/master/exception identities are refused.
`dryRun` is mandatory for adopt; a nonzero schedule is rejected. `adoptId`,
`adoptHash`, and `dryRun` are invalid on create/edit/done/delete, even `dryRun: false`.

## Offline validation (no credentials or upstream fallback)

Automated flow, real typed fetch/query layer and Base UI dialogs:

```sh
nix develop --command bash -c 'npm test -- test/hassles*.test.ts'
```

Real-browser geometry regression, with actual React SSR and built production CSS,
no server, credentials, or network fallback:

```sh
nix develop --command bash -c 'npm run build && nix shell nixpkgs#chromium --command npx --no-install tsx scripts/hassles-layout-smoke.ts'
```

The script uses Chromium from PATH and temporary isolated browser state. It checks
54 empty/short-list layouts at 320/375/480/640/960px available widths, immediately
below/at/above the fit threshold, and 2200px, with 14/16/20px root fonts. It asserts
filled/capped, independently centred lists, sidebar/stack placement, the 450px sidebar
form versus full-width stacked form, and no overflowing controls. This is rendered geometry evidence, not hydrated
keyboard/focus testing or live Calendar/phone delivery.

Hydrated offline Chromium checks (Chromium must be on PATH), using the real React
controls and typed fetch layer with separate delayed-processing and delayed-response gates:

```sh
nix develop --command bash -c 'npm run build && nix shell nixpkgs#chromium --command npx --no-install tsx scripts/hassles-optimistic-smoke.ts'
```

This checks visible optimism before processing, independent rows/Add, intermediate
readback, acknowledged removal followed by a failed inventory GET, and GET-only retry.
It uses a temporary file/profile and an in-memory fixture with no network fallback.
The flow/coordinator tests additionally cover lost create responses and exact retries
through remount, partial deletes, same-tick double clicks, location isolation, failed
edit restoration, read cutoffs and reversed write response ordering.

Browser smoke harness, using only an in-memory **test fixture**, not the real server:

```sh
nix develop --command bash -c 'npm run build && npx --no-install tsx scripts/hassles-smoke.ts'
```

Open <http://127.0.0.1:8877/hassles>. Follow its Settings link. In the Hassles
settings section select Offline account → Offline reminders → acknowledge dedicated
overlay → Use calendar, then return to Hassles using navigation. Add a title/date, repeat interval
and end; save; inspect chronological day groups; switch Upcoming/Ended; use the pencil to edit;
use the Done tick and confirm. There is no separate Delete button.
Select Hourly and each supported Every dropdown choice (including 0.5), check Work hours only, and try an invalid first
reminder (08:59, 17:00 or a weekend) before submitting a valid weekday start.
After a simulated failed create, verify interval/checkbox/date and retry UUID survive;
edit the created schedule and toggle the checkbox. No live delivery claim is implied. Tab/Shift-Tab through controls; verify dialog initial
focus, Escape to cancel, focus return and trapped tab order. Repeat at 375px and
wide desktop widths (including 960, 1200, 1600 and 1920px): controls should stay visible,
list width should fill/cap correctly without squeezing at the old 60rem breakpoint,
the list should remain centred, and dialogs should scroll.
These visual/responsive checks are a runnable review path, not a claim that a real
browser or phone has been manually tested.

Inject a write-permission error (then try Add/Save or Done):

```sh
curl -s http://127.0.0.1:8877/__hassles_fixture -H 'Content-Type: application/json' \
  -d '{"mode":"read-only"}'
```

Other modes: `conflict`, `disconnected`, `disabled`, `inventory-error`, `partial-delete`,
`normal`, `reset`. Partial deletion requires a fixture task with multiple resources. Refresh reminders
for read errors; reload for changed capability/account state. Restore `normal` and
retry the **same create request**, including after in-app navigation, to verify its UUID
and exact payload are retained. The fixture
intentionally implements only the Hassles flow; other mail sections have no fallback.
Stop it with Ctrl-C. Do not use this fixture as a service.

## Dependencies / Nix

Promotes only docket's already-pinned go-ical, go-webdav and rrule-go versions.
No dependency upgrades, OAuth reimplementation or security-pin changes.
`vendorHash` is updated for the newly reachable packages. It was derived using
`go mod vendor` + `nix hash path`; the same procedure reproduced the previous
baseline hash exactly. No deployment or host/pin changes were performed.

## Physical-occurrence completion (temporary semantics)

These two controls operate on the current Calendar-backed model, not new task types.
**All instances** retains `done`: conditionally delete every resource in the logical group.
**This instance only** targets exactly the displayed `timing.next`, or `timing.last` for
an entirely known Ended group. A server-produced nullable `instance: {at, token}` binds
that instant to its unique resource path and UID (SHA-256, NUL-delimited). This is an
identity witness, not a grant. Ownership, no participants, strong ETags, supported
unambiguous master and the existing Melbourne/timezone guards still apply. Any
incomplete member, tied selected occurrence, unsupported selected schedule, or 1,000
existing app exclusions disables the single check with a reason; owned groups can
still use All instances. A multi-resource group may complete its unique known member;
this does not make that group's schedule editable.

`POST /v1/hassles` with `action: "done-instance"` requires the selected `location`,
group `id`/`revision`, exact `instanceAt`/`instanceToken` copied from inventory, and
explicit boolean `dryRun`. No raw resource path is accepted. `dryRun: true` re-fetches
and validates under the normal selection/mutation mutex, then returns `completion`
with `instanceAt`, digest `resourceId`, `operation` (`delete-resource` or
`exclude-occurrence`) and semantic iCalendar `before`/`after` exports, with **zero
writes**. Empty `after` means deletion. Operator testing must preview before committing
with `dryRun: false` and the same bound identity/revision/witness. The UI explicitly
sends false on a user click. `adoptId`/`adoptHash` remain adopt-only; `dryRun` is valid
only for adopt/done-instance. Null or cross-action instance/adoption flags are rejected.

Commit revalidates the exact displayed witness and revision against the fresh list.
If the clock moved to another Next/Last, even without an ETag change, return conflict;
never silently complete the new occurrence. Stale/lost-response retries may return
409 and require refresh; automatic mutation retries remain off. A one-off deletes only
its selected resource. A recurring master is deep-cloned and gains one UTC EXDATE,
not a detached event or replacement resource. UID, DTSTART/DTEND, RRULE, notes, alarms,
VTIMEZONE, unknown parameters and every other group member remain intact. The sole
write uses the original strong ETag and confined selected-calendar path; no rollback
or partial deletion of other members is attempted. Both actions toast and refresh;
instance completion updates a recurring card rather than removing its whole group.

To retain editing after app completion, only a previously recognized base schedule
(or a valid existing app completion marker) gets the DESCRIPTION provenance marker
`[chainmail:completed-instances:v1]`, separate from the single hassle grouping tag.
Only bounded, distinct, well-formed EXDATE values (at most 1,000) with that marker
permit validating a clone of the base schedule with EXDATE removed. UTC values and
Google's normalized `TZID=Australia/Melbourne` form are accepted; only the optional
`VALUE=DATE-TIME` parameter is additionally allowed. Each instant must round-trip
unambiguously through the supported 1970–2100 UTC/Melbourne date profile. Unknown
zones/parameters, gaps/overlaps and duplicate equivalent instants remain restricted. Unknown imported exclusions/recurrences/timezones remain restricted;
the marker never grants ownership. Original EXDATE properties are retained through
both title and timing edits: completed instances keep their exact original UTC
instants, **not ordinal positions** in an edited schedule. Google embedded Melbourne
2008–2100 and native 1970–2100 profiles were unchanged by that normalization work.
Hourly/work-hours support is documented above; no logical-nag redesign is included. Calendar notification delivery still needs separate live review.

Single-day all-day DATE events now have read-only date timing, including bounded
DATE RDATE/EXDATE and known recurrence rules. An explicit MONTHLY BYMONTHDAY is
recognized only when its single canonical positive day equals DTSTART's day,
which is equivalent to the already-supported implicit monthly day. Other filters
remain unknown. Today's date remains Upcoming until
Melbourne midnight; next/last all-day flags mean a civil date, not a notification
hour. The UI says All day instead of displaying midnight. Malformed/mixed types,
multiday/DURATION and unsupported rules remain unknown. All-day editing and
single-instance completion remain unavailable; ownership protections are unchanged.
