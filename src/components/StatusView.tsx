import { $api, type ServiceStatus, type Stats } from "../lib/api";
import { when } from "../lib/stamp";

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
    ["chain roots", String(s.chainRoots)],
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
 * The /status route: which of the backends chainmail reads through are logged
 * in, as the operator's `corpus status` last measured them, plus the corpus
 * coverage /v1/stats already reports and the two settings that decide how it is
 * read: how often the mailbox is swept, and which folder the home page opens in.
 * The settings write; the rest is how the status screen stays on the safe side
 * of the render/model boundary: the server never contacts docket or slackdump,
 * it serves what the CLI wrote.
 */
export function StatusView() {
  const status = $api.useQuery("get", "/v1/status", {});
  const stats = $api.useQuery("get", "/v1/stats", {});
  const settings = $api.useQuery("get", "/v1/settings", {});
  // The labels the folder control offers. The same list the home page's own
  // folder button reads, for the same reason: a folder that is not in the corpus
  // is not a folder the page can open in.
  const folders = $api.useQuery("get", "/v1/labels", {});
  // The one setting on this screen, written the way the home page writes its
  // own: only the field being changed is named, and the rest are left as they
  // stand.
  const save = $api.useMutation("post", "/v1/settings", {
    onSuccess: () => {
      void settings.refetch();
      // The next sweep is computed from the cadence, so a change to one is a
      // change to the other: without this the line under the control would keep
      // naming the old schedule until the page was reloaded.
      void status.refetch();
    },
  });
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

      {/* The settings: the two things about reading this corpus that are the
          reader's rather than the mail's, each written where it is read. They
          belong next to the backends they decide the reading of, and they are
          the only controls on this screen — everything else here reports. */}
      <h2 className="sthead">Settings</h2>
      <p className="stnote">
        The mailbox is swept{" "}
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
        {status.data?.nextSlurpAt ? (
          <> — next {when(status.data.nextSlurpAt)}.</>
        ) : every === "off" ? (
          <> — never, unless asked.</>
        ) : every === "" ? null : (
          // A cadence with nothing scheduled is a host whose -slurp grant is off,
          // which is a state the page cannot fix and should not hide.
          <> — nothing scheduled.</>
        )}
      </p>
      {/* The setting the home page writes when a reader says "open this folder by
          default". It is here as well because this is the page that lists what
          the server does; a reader who wants the corpus to open somewhere should
          not have to remember it was a switch in a menu on another screen. */}
      <p className="stnote">
        The home page opens in{" "}
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
        .
      </p>
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
    </div>
  );
}