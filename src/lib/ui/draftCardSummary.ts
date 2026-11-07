/** A bounded opening preview and whitespace-delimited word count; never changes the draft. */
export function draftCardSummary(body?: string): { preview: string; wordCount: number } {
  const text = body?.trim() ?? "";
  const words = text ? text.split(/\s+/u) : [];
  const opening = Array.from(words.slice(0, 12).join(" "));
  const shortened = words.length > 12 || opening.length > 100;
  return {
    preview: opening.slice(0, 100).join("") + (shortened ? "…" : ""),
    wordCount: words.length,
  };
}
