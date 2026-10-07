import type { Stats } from "../../lib/api/api";
import SettingsSection from "./SettingsSection";
import { errText } from "./errText";

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
    <dl className="mt-0.5 grid grid-cols-[repeat(auto-fit,minmax(9rem,1fr))] gap-2">
      {rows.map(([term, definition]) => (
        <div className="min-w-0 rounded-lg border border-line bg-bg px-3 py-3" key={term}>
          <dt className="break-words text-[.63rem] font-bold uppercase tracking-[.07em] text-muted">
            {term}
          </dt>
          <dd className="mt-1 break-words text-base font-semibold tabular-nums">{definition}</dd>
        </div>
      ))}
    </dl>
  );
}

export default function SettingsCorpus({
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
          className="mt-3 rounded-sm border-l-[3px] border-l-red-700 bg-bg px-3 py-2 text-[.76rem] leading-[1.45] text-fg break-words mb-0"
          role="alert"
        >
          {errText(stats.error)}
        </p>
      ) : stats.data ? (
        <CorpusStats stats={stats.data} />
      ) : (
        <p className="mt-3 mb-0 text-[.76rem] leading-[1.45] text-muted">Reading the corpus…</p>
      )}
    </SettingsSection>
  );
}
