import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { StatusResponse } from "../src/lib/api/api";
import { embeddingIndicator } from "../src/lib/ops/embeddingIndicator";
import NavRefresh from "../src/components/navigation/NavRefresh";

const fixture = vi.hoisted(() => ({ status: undefined as StatusResponse | undefined }));
vi.mock("../src/lib/api/api", () => ({
  ApiError: class extends Error {},
  $api: {
    useQuery: () => ({ data: fixture.status }),
    useMutation: () => ({ mutateAsync: async () => ({}) }),
  },
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));

const idle: StatusResponse["embedding"] = {
  enabled: true,
  running: false,
  queued: false,
  model: "test-model",
  dim: 8,
  done: 0,
  pending: 0,
  embedded: 0,
  skipped: 0,
};

describe("independent semantic indexing indicator", () => {
  it.each([
    [undefined, null],
    [{ ...idle, enabled: false }, null],
    [idle, null],
    [{ ...idle, outcome: "complete" }, null],
    [{ ...idle, queued: true }, "semantic indexing queued"],
    [{ ...idle, running: true }, "indexing semantics"],
    [{ ...idle, outcome: "failed", error: "model broke" }, "semantic indexing delayed"],
    [{ ...idle, outcome: "skipped", error: "daemon down" }, "semantic indexing delayed"],
  ] as const)("composes %j as %s", (status, expected) => {
    expect(embeddingIndicator(status)?.label ?? null).toBe(expected);
  });

  it("reports committed progress without pretending the baseline is live coverage", () => {
    const result = embeddingIndicator({
      ...idle,
      running: true,
      queued: true,
      done: 3,
      pending: 9,
    });
    expect(result?.title).toContain("3/9 committed; another pass queued");
    expect(result?.title).toContain("refresh does not wait");
  });

  it.each([
    [{ running: true }, "ingesting"],
    [{ running: false, outcome: "failed" }, "ingest failed"],
    [{ running: false, outcome: "incomplete" }, "ingest incomplete"],
  ] as const)("keeps ingest state visible alongside indexing: %j", (sweep, text) => {
    fixture.status = {
      services: [{ id: "embed", label: "Embedding", status: "unchecked" }],
      sweep,
      embedding: { ...idle, running: true, done: 3, pending: 9 },
    };
    const html = renderToStaticMarkup(createElement(NavRefresh));
    expect(html).toContain(text);
    expect(html).toContain("indexing semantics");
    expect(html).toContain('role="status"');
    expect(html).toContain("3/9 committed");
    expect(html).not.toMatch(/ disabled(?:=|>)/);
  });

  it("does not conflate daemon health with worker status or disable refresh on failure", () => {
    fixture.status = {
      services: [{ id: "embed", label: "Embedding", status: "ok" }],
      sweep: { running: false },
      embedding: { ...idle, outcome: "failed", error: "index failed" },
    };
    const html = renderToStaticMarkup(createElement(NavRefresh));
    expect(html).toContain("semantic indexing delayed");
    expect(html).toContain("index failed");
    expect(html).not.toMatch(/ disabled(?:=|>)/);
    expect(html).toContain('aria-busy="false"');
  });
});
