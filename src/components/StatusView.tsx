import { $api, type ServiceStatus, type Stats } from "../lib/api";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
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

/** A UTC RFC3339 stamp shown in the reader's own local time: date and clock,
 *  year dropped when it is the current one, so a recent check reads fresh. */
function when(stamp: string): string {
  const d = new Date(stamp);
  if (Number.isNaN(d.getTime())) return stamp;
  const nowY = new Date().getFullYear();
  const date = d.getFullYear() === nowY
    ? d.toLocaleDateString(undefined, { day: "numeric", month: "short" })
    : d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
  const t = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  return `${date}, ${t}`;
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
 * coverage /v1/stats already reports. Both halves are read-only — this is how
 * the status screen stays on the safe side of the render/model boundary: the
 * server never contacts docket or slackdump; it serves what the CLI wrote.
 */
export function StatusView() {
  const status = $api.useQuery("get", "/v1/status", {});
  const stats = $api.useQuery("get", "/v1/stats", {});

  return (
    <div className="wrap statuswrap">
      <header className="top">
        <h1>chainmail</h1>
        <p className="sub">
          Which services this machine is logged into, and what the corpus holds.
        </p>
      </header>

      <h2 className="sthead">Logged in</h2>
      <p className="stnote">
        Run <code>corpus status</code> to re-measure.
        {status.data?.checkedAt ? <> Last checked {when(status.data.checkedAt)}.</>
          : " Nothing measured yet."}
        {status.data?.nextSlurpAt ? <> Next slurp {when(status.data.nextSlurpAt)}.</> : null}
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