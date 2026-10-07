import type { ServiceStatus, Stats, StatusResponse } from "../lib/api";
import { when } from "../lib/stamp";
import { SettingsSection } from "./SettingsScaffold";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/** A service's reported state and detail, as one settings-list row. */
function ServiceRow({ svc }: { svc: ServiceStatus }) {
  const badges: Record<ServiceStatus["status"], { word: string; className: string }> = {
    ok: {
      word: "logged in",
      className:
        "inline-flex whitespace-nowrap rounded-full border border-current px-[.48rem] py-[.13rem] text-[.64rem] font-bold text-green-800",
    },
    "needs-auth": {
      word: "needs auth",
      className:
        "inline-flex whitespace-nowrap rounded-full border border-current px-[.48rem] py-[.13rem] text-[.64rem] font-bold text-amber-700",
    },
    down: {
      word: "down",
      className:
        "inline-flex whitespace-nowrap rounded-full border border-current px-[.48rem] py-[.13rem] text-[.64rem] font-bold text-red-700",
    },
    unchecked: {
      word: "unchecked",
      className:
        "inline-flex whitespace-nowrap rounded-full border border-current px-[.48rem] py-[.13rem] text-[.64rem] font-bold text-[var(--muted)]",
    },
  };
  const badge = badges[svc.status] ?? badges.unchecked;
  return (
    <li className="flex flex-wrap items-center gap-x-[.7rem] gap-y-[.45rem] py-[.55rem]">
      <span className={badge.className}>{badge.word}</span>
      <span className="min-w-32 text-[.8rem] font-semibold">{svc.label}</span>
      {svc.detail ? (
        <span className="flex-[1_1_12rem] break-words text-[.72rem] text-[var(--muted)]">
          {svc.detail}
        </span>
      ) : null}
    </li>
  );
}

function CorpusStats({ stats }: { stats: Stats }) {
  const rows: [string, string][] = [
    ["entries", String(stats.entries)],
    ["people", String(stats.people)],
    ["thread roots", String(stats.chainRoots)],
    ["unresolved", String(stats.unresolved)],
  ];
  for (const [source, count] of Object.entries(stats.bySource)) {
    rows.push([`${source} entries`, String(count)]);
  }
  for (const model of stats.embeddings) {
    rows.push([`embeddings · ${model.model}`, `${model.vectors} vectors`]);
  }
  return (
    <dl className="mt-[.1rem] grid grid-cols-[repeat(auto-fit,minmax(9rem,1fr))] gap-2">
      {rows.map(([term, definition]) => (
        <div
          className="min-w-0 rounded-lg border border-[var(--line)] bg-[var(--bg)] px-[.7rem] py-[.65rem]"
          key={term}
        >
          <dt className="break-words text-[.63rem] font-bold uppercase tracking-[.07em] text-[var(--muted)]">
            {term}
          </dt>
          <dd className="mt-[.3rem] break-words text-base font-semibold tabular-nums">
            {definition}
          </dd>
        </div>
      ))}
    </dl>
  );
}

/** Service health and corpus snapshot are read-only sections with independent data. */
export function SettingsServices({
  status,
}: {
  status: {
    data?: StatusResponse;
    isError: boolean;
    error: unknown;
    isPending: boolean;
  };
}) {
  return (
    <SettingsSection
      id="services-heading"
      title="Connected services"
      description={`Run corpus status to refresh.${status.data?.checkedAt ? ` Last checked ${when(status.data.checkedAt)}.` : " Nothing measured yet."}`}
    >
      {status.isError ? (
        <p
          className="mt-[.65rem] rounded-sm border-l-[3px] border-l-red-700 bg-[var(--bg)] px-[.65rem] py-2 text-[.76rem] leading-[1.45] text-[var(--fg)] break-words mb-0"
          role="alert"
        >
          {errText(status.error)}
        </p>
      ) : null}
      <ul className="mt-[.7rem] list-none border-t border-[var(--line)] p-0">
        {status.isPending ? (
          <li className="flex flex-wrap items-center gap-x-[.7rem] gap-y-[.45rem] py-[.55rem] text-[.78rem] text-[var(--muted)]">
            Checking services…
          </li>
        ) : status.data?.services.length ? (
          status.data.services.map((svc) => <ServiceRow key={svc.id} svc={svc} />)
        ) : (
          <li className="flex flex-wrap items-center gap-x-[.7rem] gap-y-[.45rem] py-[.55rem] text-[.78rem] text-[var(--muted)]">
            No services reported.
          </li>
        )}
      </ul>
    </SettingsSection>
  );
}

export function SettingsCorpus({
  stats,
}: {
  stats: { data?: Stats; isError: boolean; error: unknown };
}) {
  return (
    <SettingsSection
      id="corpus-heading"
      title="Corpus"
      description="A snapshot of the mail and identities currently stored."
    >
      {stats.isError ? (
        <p
          className="mt-[.65rem] rounded-sm border-l-[3px] border-l-red-700 bg-[var(--bg)] px-[.65rem] py-2 text-[.76rem] leading-[1.45] text-[var(--fg)] break-words mb-0"
          role="alert"
        >
          {errText(stats.error)}
        </p>
      ) : stats.data ? (
        <CorpusStats stats={stats.data} />
      ) : (
        <p className="mt-[.65rem] mb-0 text-[.76rem] leading-[1.45] text-[var(--muted)]">
          Reading the corpus…
        </p>
      )}
    </SettingsSection>
  );
}
