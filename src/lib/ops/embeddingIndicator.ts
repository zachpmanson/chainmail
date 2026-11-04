import type { components } from "../api/api.d";

// Ingest and daemon health deliberately do not participate in this indicator.
export function embeddingIndicator(status?: components["schemas"]["EmbeddingStatus"]) {
  if (!status?.enabled) return null;
  const progress = `${status.done}/${status.pending} committed`;
  if (status.running) {
    return {
      label: "indexing semantics",
      title: `Semantic indexing: ${progress}${status.queued ? "; another pass queued" : ""}. Lexical search is available; refresh does not wait for indexing.`,
    };
  }
  if (status.outcome === "failed" || status.outcome === "skipped") {
    return {
      label: "semantic indexing delayed",
      title: `Semantic indexing ${status.outcome}: ${status.error || "unavailable"}. Lexical search is available. The next ingest retries indexing.`,
    };
  }
  if (status.queued) {
    return {
      label: "semantic indexing queued",
      title: "Lexical search is available; semantic indexing is queued.",
    };
  }
  return null;
}
