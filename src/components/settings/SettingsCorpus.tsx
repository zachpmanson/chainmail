import type { Stats } from "../../lib/api/api";
import SettingsSection from "./SettingsSection";
import InlineAlert from "../ui/InlineAlert";
import { errText } from "../../lib/ui/errText";

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
        <div className="min-w-0 rounded-lg border border-line bg-bg p-3" key={term}>
          <dt className="wrap-break-word text-2xs font-bold uppercase tracking-[.07em] text-muted">
            {term}
          </dt>
          <dd className="mt-1 wrap-break-word text-base font-semibold tabular-nums">
            {definition}
          </dd>
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
        <InlineAlert compact>{errText(stats.error)}</InlineAlert>
      ) : stats.data ? (
        <CorpusStats stats={stats.data} />
      ) : (
        <p className="mt-3 mb-0 text-xs/normal text-muted">Reading the corpus…</p>
      )}
    </SettingsSection>
  );
}
