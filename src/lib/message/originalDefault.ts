export type OriginalAnalysis = {
  score: number;
  matches: { rule: string; points: number }[];
  defaultsToOriginal: boolean;
};

/** Analyse a detached, inert copy; signatures stay intact in the displayed original. */
export function analyseOriginal(html: string): OriginalAnalysis {
  const template = document.createElement("template");
  template.innerHTML = html;
  const body = template.content;
  // These are the same explicit markers used by the collector's signature handling.
  // Unmarked sign-offs are not evidence: keep them rather than guessing at prose.
  body.querySelectorAll(".gmail_signature, details.sig").forEach((signature) => signature.remove());
  const matches: OriginalAnalysis["matches"] = [];
  if (body.querySelector("table table")) matches.push({ rule: "nested tables", points: 2 });
  if (body.querySelectorAll("[class], [style]").length >= 3)
    matches.push({ rule: "3+ styled elements", points: 1 });
  if (body.querySelector("img") && body.querySelectorAll("a").length >= 2)
    matches.push({ rule: "image + 2+ links", points: 1 });
  const score = matches.reduce((sum, match) => sum + match.points, 0);
  return { score, matches, defaultsToOriginal: score >= 3 };
}

export function originalHtmlScore(html: string): number {
  return analyseOriginal(html).score;
}

export function defaultsToOriginal(html: string): boolean {
  return analyseOriginal(html).defaultsToOriginal;
}
