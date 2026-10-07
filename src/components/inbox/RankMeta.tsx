import { type ChainHit } from "../../lib/api/api";

/** The thread's best cosine similarity to the query, from its best entry hits. */
export function threadSimilarity(thread: ChainHit): number {
  let best = 0;
  for (const e of thread.best ?? []) {
    if (e.semRank > 0 && e.similarity !== undefined && e.similarity > best) best = e.similarity;
  }
  return best;
}

export function spanOf(thread: ChainHit): string {
  const day = (t?: string) => (t ? t.slice(0, 10) : "");
  const a = day(thread.first);
  const b = day(thread.last);
  if (!a && !b) return "undated";
  if (!b || a === b) return a || b;
  return `${a} – ${b}`;
}

export default function RankMeta({ thread }: { thread: ChainHit }) {
  const sim = threadSimilarity(thread);
  return (
    <>
      <span className="font-semibold text-fg" title="matching entries of the whole thread">
        {thread.matched} of {thread.entries} matched
      </span>
      {sim > 0 ? (
        <span className="font-semibold text-strong" title="best cosine similarity of the thread">
          sim {sim.toFixed(2)}
        </span>
      ) : null}
      <span className="ml-auto tabular-nums" title="the thread's first and last message">
        {spanOf(thread)}
      </span>
    </>
  );
}
