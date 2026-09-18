import { useQueryClient } from "@tanstack/react-query";
import { $api, type PersonSummary, type ServiceStatus, type Stats } from "../lib/api";
import { when } from "../lib/stamp";
import { Palette } from "./Palette";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/**
 * The cadences the sweep control offers, as the words the server stores and
 * serves them in (cmd/server/schedule.go). One vocabulary, listed once: a
 * control whose options the server canonicalises differently would be handing
 * back a value it then could not show.
 *
 * "Never" is a cadence rather than the absence of one: the mailbox is then read
 * only when someone asks for it, which is a choice a reader can want and the one
 * the refresh button already offers.
 */
const CADENCES: [string, string][] = [
  ["5m", "5 minutes"],
  ["10m", "10 minutes"],
  ["15m", "15 minutes"],
  ["30m", "30 minutes"],
  ["1h", "every hour"],
  ["6h", "every 6 hours"],
  ["off", "never"],
];

/**
 * The options, with the value in force appended when it is not one of them. The
 * server takes any whole-minute cadence between a minute and a day, so one set
 * through the API has to leave the control able to show it rather than silently
 * snapping to the first option and writing that back.
 */
function cadenceOptions(every: string): [string, string][] {
  return CADENCES.some(([word]) => word === every) ? CADENCES : [...CADENCES, [every, every]];
}

/**
 * The mailboxes the default-folder control offers: every label the mailbox has
 * put on something, plus the choice of none. "All mail" is the words the home
 * page's own folder button uses for it, and the setting is the same one — this
 * is the second place it can be seen and set, not a second setting.
 *
 * The value in force is appended when it is not a label the corpus knows: the
 * server does not validate the folder against the label list (it may be one the
 * next sweep brings in), so a control that only listed the labels could show
 * nothing for the folder it is actually opening in.
 */
function folderOptions(labels: { name: string }[], folder: string): [string, string][] {
  const opts: [string, string][] = [
    ["", "All mail"],
    ...labels.map((l) => [l.name, l.name] as [string, string]),
  ];
  return folder === "" || labels.some((l) => l.name === folder) ? opts : [...opts, [folder, folder]];
}

/**
 * The addresses a person is, out of the "kind:value" identities the corpus
 * serves. Only `email:` — a Slack uid is not a mailbox a message can be marked
 * as coming from, and a display name borrowed from somebody else's quote is a
 * placeholder for a person nobody has identified yet.
 */
function emailIdentities(p: PersonSummary): string[] {
  return (p.identities ?? [])
    .filter((i) => i.startsWith("email:"))
    .map((i) => i.slice("email:".length));
}

/**
 * The value the reader control shows for a corpus whose setting is the address
 * list it was configured with before this control named a person. It is not a
 * person id and is never sent anywhere: a change away from it writes whichever
 * person was picked, so the state is shown honestly and left behind in one click.
 */
const STORED_ADDRESSES = "addresses";

/**
 * Who the reader is, as the options of the one control that says so, and which of
 * them the setting names.
 *
 * A person rather than a field of addresses, because the addresses are the
 * corpus's to fold and not the reader's to retype: one account behind
 * zach@loomworks.example and zach@millrace.example is one person in the identity graph,
 * and a field asked the reader to do that folding by hand, again for every alias
 * they would rather their own mail were marked from.
 *
 * Only people the corpus has an address for are offered — a person known only by
 * a display name recovered from somebody else's quote has no mailbox for mail to
 * have come from, and the API refuses one too — and they come in the corpus's own
 * order, most involved first, which is where the reader of their own corpus is.
 *
 * The value is the id, and the addresses in force come back from the server
 * resolved: what a control wrote is the person, and which aliases that is today is
 * the corpus's answer rather than the page's (see the sentence under it).
 */
function meOptions(
  people: PersonSummary[],
  personId: number | undefined,
  addresses: string[],
): { options: [string, string][]; value: string; person: string; addresses: string[] } {
  const options: [string, string][] = [["", "Nobody"]];
  let person = "";
  for (const p of people) {
    if (emailIdentities(p).length === 0) continue;
    options.push([String(p.personId), p.displayName]);
    if (p.personId === personId) person = p.displayName;
  }
  let value = personId === undefined ? "" : String(personId);
  if (value === "" && addresses.length > 0) {
    // The address list a corpus was configured with before the setting was a
    // person: shown as the value it is rather than as nobody, and appended the
    // way the folder and cadence controls keep a value they cannot name.
    value = STORED_ADDRESSES;
    options.push([STORED_ADDRESSES, addresses.join(", ")]);
  }
  return { options, value, person, addresses };
}

/**
 * The addresses being marked, under the name of the person they are: "Ada Byron:
 * ada@loomworks.example". Either half can be missing — a corpus configured before
 * this control had a person to name has only addresses, and the two fields arrive
 * from two different answers — so the line is built from what is there rather
 * than from a template that would print a colon with nothing after it. What is
 * left is still the answer to whose mail this is.
 */
function meLine(me: { person: string; addresses: string[] }): string {
  return [me.person, me.addresses.join(", ")].filter((s) => s !== "").join(": ");
}

/**
 * A badge's wording and colour, per the state the probe reported. A state is
 * a truth the screen is asserting, so it earns a colour; "unchecked" is the
 * calm first-boot grey rather than an error, because nothing has been asked
 * yet and the answer is not "no".
 */
const BADGES: Record<ServiceStatus["status"], { word: string; cls: string }> = {
  ok: { word: "logged in", cls: "stbad st-ok" },
  "needs-auth": { word: "needs auth", cls: "stbad st-na" },
  down: { word: "down", cls: "stbad st-down" },
  unchecked: { word: "unchecked", cls: "stbad st-un" },
};

function OneRow({ svc }: { svc: ServiceStatus }) {
  const badge = BADGES[svc.status] ?? BADGES.unchecked;
  return (
    <li className="strow">
      <span className={badge.cls}>{badge.word}</span>
      <span className="stlabel">{svc.label}</span>
      {svc.detail ? <span className="stdetail">{svc.detail}</span> : null}
    </li>
  );
}

function CorpusStats({ s }: { s: Stats }) {
  const rows: [string, string][] = [
    ["entries", String(s.entries)],
    ["people", String(s.people)],
    ["thread roots", String(s.chainRoots)],
    ["unresolved", String(s.unresolved)],
  ];
  for (const [src, n] of Object.entries(s.bySource)) {
    rows.push([`${src} entries`, String(n)]);
  }
  for (const m of s.embeddings) {
    rows.push([`embeddings · ${m.model}`, `${m.vectors} vectors`]);
  }
  return (
    <dl className="stdl">
      {rows.map(([term, def]) => (
        <div className="stdrow" key={term}>
          <dt>{term}</dt>
          <dd>{def}</dd>
        </div>
      ))}
    </dl>
  );
}

/**
 * The /settings route: which of the backends chainmail reads through are logged
 * in, as the operator's `corpus status` last measured them, plus the corpus
 * coverage /v1/stats already reports and the two settings that decide how it is
 * read: how often the mailbox is swept, which folder the home page opens in, and
 * which person's mail is the reader's own.
 * The settings write; the rest is how the status screen stays on the safe side
 * of the render/model boundary: the server never contacts docket or slackdump,
 * it serves what the CLI wrote.
 */
export function SettingsView() {
  const queryClient = useQueryClient();
  const status = $api.useQuery("get", "/v1/status", {});
  const stats = $api.useQuery("get", "/v1/stats", {});
  const settings = $api.useQuery("get", "/v1/settings", {});
  // The labels the folder control offers. The same list the home page's own
  // folder button reads, for the same reason: a folder that is not in the corpus
  // is not a folder the page can open in.
  const folders = $api.useQuery("get", "/v1/labels", {});
  // The settings on this screen, written the way the home page writes its own:
  // only the field being changed is named, and the rest are left as they stand.
  const save = $api.useMutation("post", "/v1/settings", {
    onSuccess: (_stored, variables) => {
      void settings.refetch();
      // The next sweep is computed from the cadence, so a change to one is a
      // change to the other: without this the line under the control would keep
      // naming the old schedule until the page was reloaded.
      void status.refetch();
      // The pane's marks are resolved from the stored person, so a write that
      // names them has to redraw the open thread: without this the reader says
      // who they are and the thread they are looking at keeps showing their own
      // mail unmarked until they reload. The other settings change nothing the
      // pane draws, and re-reading a thread for one would be a request per click
      // on a control that has nothing to do with threads.
      if (variables.body?.mePersonId !== undefined) {
        void queryClient.invalidateQueries({ queryKey: ["get", "/v1/chains/{rootExtId}"] });
      }
    },
  });
  // Persist who the reader is, as the person the control offered. What travels is
  // the person's id and not their addresses: which addresses are one human is the
  // identity graph's reading, and a client that sent a list would be storing a
  // second one that goes stale the moment an alias is learned. Only `mePersonId`
  // is named in the body, which is what leaves the cadence and the folder as they
  // stand, and "Nobody" is a zero — which is not a person id, so clearing is the
  // same field with the same meaning.
  const saveMe = (picked: string) => save.mutate({ body: { mePersonId: Number(picked) } });
  // The people the corpus holds, which is where the reader picks themselves: the
  // identity graph is the only thing in corpus that knows two addresses are one
  // person, and the setting is that question asked once.
  const people = $api.useQuery("get", "/v1/people", {});
  const me = meOptions(
    people.data?.people ?? [],
    settings.data?.mePersonId,
    settings.data?.me ?? [],
  );
  // Which the sentence under the control can be written from yet: the settings are
  // what say who is in force and which addresses that means, and the people are
  // what names them, so until both have answered the line says so rather than
  // calling the reader nobody.
  const meKnown = !settings.isPending && !people.isPending;
  const every = settings.data?.slurpEvery ?? "";
  const folder = settings.data?.defaultFolder ?? "";
  const labels = folders.data?.labels ?? [];
  const busy = save.isPending || settings.isPending;

  return (
    <div className="wrap statuswrap">
      <h2 className="sthead">Logged in</h2>
      <p className="stnote">
        Run <code>corpus status</code> to re-measure.
        {status.data?.checkedAt ? <> Last checked {when(status.data.checkedAt)}.</>
          : " Nothing measured yet."}
      </p>
      {status.isError ? (
        <p className="selfail" role="alert">
          {errText(status.error)}
        </p>
      ) : null}
      <ul className="stlist">
        {status.data && status.data.services.length > 0 ? (
          status.data.services.map((svc) => <OneRow key={svc.id} svc={svc} />)
        ) : (
          <li className="strow stempty">Checking services…</li>
        )}
      </ul>

      {/* The settings: the things about reading this corpus that are the
          reader's rather than the mail's, each written where it is read. They
          belong next to the backends they decide the reading of, and they are
          the only controls on this screen — everything else here reports.

          Three columns: the name of the setting, the control that sets it, and
          what the setting knows about itself — the schedule the cadence implies,
          the addresses the reader's mail comes from. The third was inside the
          second's cell, trailing each control at whatever width the control
          happened to end at, which read as three unrelated asides down a ragged
          edge; given a column of its own, the commentary is read down as one and
          compared, which is why it was written beside the control at all. A
          setting with nothing to add leaves the cell empty rather than closing
          the column up — the names and the controls stay in their own columns
          whatever any one row says. */}
      <h2 className="sthead">Settings</h2>
      <dl className="stdl stset">
        <dt>Sweep the mailbox</dt>
        <dd>
          <select
            className="stpick"
            aria-label="How often to sweep the mailbox"
            value={every}
            disabled={busy || every === ""}
            onChange={(e) => save.mutate({ body: { slurpEvery: e.target.value } })}
          >
            {every === "" ? <option value="">…</option> : null}
            {cadenceOptions(every).map(([word, label]) => (
              <option key={word} value={word}>
                {label}
              </option>
            ))}
          </select>
        </dd>
        {status.data?.nextSlurpAt ? (
          <dd className="sttail">next {when(status.data.nextSlurpAt)}</dd>
        ) : every === "off" ? (
          <dd className="sttail">never, unless asked</dd>
        ) : every === "" ? (
          // Nothing read yet, so nothing to say about the cadence: the cell is
          // empty, as the folder's is.
          <dd className="sttail" />
        ) : (
          // A cadence with nothing scheduled is a host whose -slurp grant is
          // off, which is a state the page cannot fix and should not hide.
          <dd className="sttail">nothing scheduled</dd>
        )}
        {/* The setting the home page writes when a reader says "open this folder by
            default". It is here as well because this is the page that lists what
            the server does; a reader who wants the corpus to open somewhere should
            not have to remember it was a switch in a menu on another screen. */}
        <dt>Home folder</dt>
        <dd>
          <select
            className="stpick"
            aria-label="Which folder the home page opens in"
            value={settings.isPending ? "" : folder}
            disabled={busy}
            onChange={(e) => save.mutate({ body: { defaultFolder: e.target.value } })}
          >
            {settings.isPending ? <option value="">…</option> : null}
            {folderOptions(labels, folder).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </dd>
        {/* The folder control has nothing about itself to add, and its cell is
            still there: a row that left the cell out would be the one row whose
            third column starts a column to the left, because a grid places an
            unplaced item in the next free cell — the next row's name would land
            where this row's commentary did. */}
        <dd className="sttail" />
        {/* Who the reader is. A setting rather than a field on the page being built:
            the same person decides which messages are marked as the reader's
            wherever mail is read — the pane, a built page, every thread — and most
            of those have no build anywhere near them. Nothing in the corpus records
            which mailbox it was collected from, so this can only be told.

            A person is picked and the addresses come with them, read out of the
            identity graph on each read rather than stored beside the setting: the
            note after the control names them, so the reader can see which aliases
            are being marked — including the ones the corpus learned after they said
            who they were — rather than having to trust that it folded the right ones
            together.

            Capped at 20ch, like the nav's Person control and for the same reason:
            the value is a name that may carry the aliases behind it, and an
            unusually long one must not be what sets the width of the column it is
            read in. */}
        <dt>Your mail comes from</dt>
        <dd>
          <select
            className="stpick stperson"
            aria-label="Which person you are"
            value={me.value}
            disabled={busy || people.isPending}
            onChange={(e) => saveMe(e.target.value)}
          >
            {/* No options until the people are read: a control that listed Nobody
                while the answer was in flight, and then swapped it for the reader's
                own name, would be showing a setting that is not in force. It is
                disabled and blank for that moment, and the note says why. */}
            {people.isPending
              ? null
              : me.options.map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
          </select>
        </dd>
        {!meKnown ? (
          // The control is blank until both answers are in, and this is what says
          // why: the people are what name the person it would show, and the
          // settings are what say whether anyone is named at all.
          <dd className="sttail">reading…</dd>
        ) : me.value === "" ? (
          <dd className="sttail">nobody, so nothing is marked as yours.</dd>
        ) : (
          <dd className="sttail">{meLine(me)}.</dd>
        )}
      </dl>
      {people.isError ? (
        <p className="selfail" role="alert">
          {errText(people.error)}
        </p>
      ) : null}
      {save.isError ? (
        <p className="selfail" role="alert">
          {errText(save.error)}
        </p>
      ) : null}

      <h2 className="sthead">Corpus</h2>
      {stats.isError ? (
        <p className="selfail" role="alert">
          {errText(stats.error)}
        </p>
      ) : stats.data ? (
        <CorpusStats s={stats.data} />
      ) : (
        <p className="stnote">Reading the corpus…</p>
      )}

      {/* The palette, last and after everything that reports: it says nothing
          about the corpus or the operator, and nothing on this screen is read
          through it — it is here because this is the page somebody with the app
          open goes looking for the colours of the thing in front of them, and
          because the values it prints are the stylesheet's own rather than a
          copy of it (see lib/palette). */}
      <h2 className="sthead">Palette</h2>
      <p className="stnote">
        The colours in force, and in the other theme, as the browser resolves them.
      </p>
      <Palette />
    </div>
  );
}