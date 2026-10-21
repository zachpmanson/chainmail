import type { OriginalAnalysis as Analysis } from "../../lib/message/original";

export default function OriginalAnalysis({
  analysis,
  pending,
  explicit,
  on,
}: {
  analysis: Analysis | undefined;
  pending: boolean;
  explicit: boolean;
  on: boolean;
}) {
  return (
    <span className="basis-full text-2xs text-muted" data-original-analysis>
      {analysis
        ? `HTML ${analysis.score} pt — ${
            analysis.matches.length
              ? analysis.matches.map(({ rule, points }) => `${rule} (+${points})`).join(", ")
              : "no matches"
          }`
        : `HTML score ${pending ? "loading…" : "unavailable"}`}
      {explicit
        ? ` · saved ${on ? "original" : "stripped"} view (overrides score)`
        : analysis
          ? ` · automatic ${analysis.defaultsToOriginal ? "original" : "stripped"} (${analysis.uncertain ? "uncertain history" : "main layout + design evidence required"})`
          : ""}
    </span>
  );
}
