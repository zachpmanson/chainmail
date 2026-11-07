import { draftCardSummary } from "../../lib/ui/draftCardSummary";

export default function BodyWordCount({ body }: { body: string }) {
  const { wordCount } = draftCardSummary(body);
  return (
    <span
      data-word-count
      className="mr-auto w-24 shrink-0 text-xs whitespace-nowrap text-muted tabular-nums"
    >
      {wordCount} {wordCount === 1 ? "word" : "words"}
    </span>
  );
}
