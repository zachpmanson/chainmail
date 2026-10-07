import createFetchClient, { type Middleware } from "openapi-fetch";
import createQueryClient from "openapi-react-query";
import type { components, paths } from "./api.d";

/** Carries the server's own message, which is the actionable part. */
export class ApiError extends Error {
  readonly status: number;
  /** From Retry-After, in ms. Set only when the service said when to come back. */
  readonly retryAfterMs?: number;
  constructor(status: number, message: string, retryAfterMs?: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    if (retryAfterMs !== undefined) this.retryAfterMs = retryAfterMs;
  }
}

/** Seconds or an HTTP date; undefined rather than a guessed interval. */
function retryAfter(response: Response): number | undefined {
  const raw = response.headers.get("retry-after");
  if (raw === null) return undefined;
  const seconds = Number(raw.trim());
  if (Number.isFinite(seconds) && seconds >= 0) return seconds * 1000;
  const at = Date.parse(raw);
  if (Number.isNaN(at)) return undefined;
  return Math.max(0, at - Date.now());
}

/** Every API error is a stable answer except 429, which says when to retry. */
export function isAnswer(err: unknown): boolean {
  return err instanceof ApiError && err.status !== 429;
}

/** The `{error: string}` body the service uses; falls back to whatever arrived. */
function messageOf(body: unknown, status: number, statusText: string): string {
  if (body && typeof body === "object" && "error" in body) {
    const e = (body as { error?: unknown }).error;
    if (typeof e === "string" && e.trim() !== "") return e;
  }
  if (typeof body === "string" && body.trim() !== "") return body.trim();
  return statusText.trim() !== "" ? `${status} ${statusText}` : `HTTP ${status}`;
}

// Throw here, while status and Retry-After are available: openapi-react-query
// rethrows only the parsed body. Safe to consume the body because this throws.
const declineAsError: Middleware = {
  async onResponse({ response }) {
    if (response.ok) return undefined;
    let body: unknown = await response.text();
    try {
      body = JSON.parse(body as string);
    } catch {
      // Not JSON: the text stands as the message.
    }
    throw new ApiError(
      response.status,
      messageOf(body, response.status, response.statusText),
      retryAfter(response),
    );
  },
};

// Same origin: the dev server proxies /v1, so no CORS. fetch is looked up per
// call so tests that replace the global are seen.
const fetchClient = createFetchClient<paths>({
  // Absolute, because outside a browser Request rejects a relative URL.
  baseUrl: typeof location === "undefined" ? "http://127.0.0.1/" : location.origin,
  fetch: (request) => globalThis.fetch(request),
});
fetchClient.use(declineAsError);

// Query keys include every parameter, so a mode switch can't show stale results.
export const $api = createQueryClient(fetchClient);

export type SearchMode = "lexical" | "semantic" | "hybrid";

export type ChainHit = components["schemas"]["ChainHit"];

export type EntryHit = components["schemas"]["EntryHit"];

export type CorpusEntry = components["schemas"]["CorpusEntry"];

export type ServiceStatus = components["schemas"]["ServiceStatus"];

export type StatusResponse = components["schemas"]["StatusResponse"];

export type Stats = components["schemas"]["Stats"];

export type MailActionRequest = components["schemas"]["MailActionRequest"];

export type MailActionResponse = components["schemas"]["MailActionResponse"];

export type MarkReadRequest = components["schemas"]["MarkReadRequest"];

export type MarkReadResponse = components["schemas"]["MarkReadResponse"];

export type RefreshReport = components["schemas"]["RefreshReport"];

/** What one message's media pull did: the counts, and one row per file. */
export type MediaPull = components["schemas"]["MediaPullResponse"];

export type RefreshCandidate = components["schemas"]["RefreshCandidate"];

export type SendResponse = components["schemas"]["SendResponse"];

export type ComposeResponse = components["schemas"]["ComposeResponse"];

export type OpsPlanResponse = components["schemas"]["OpsPlanResponse"];

export type OpsMerge = components["schemas"]["OpsMerge"];

export type OpsMergeRecord = components["schemas"]["OpsMergeRecord"];

export type PersonSummary = components["schemas"]["PersonSummary"];

export type OrgRule = components["schemas"]["OrgRuleResponse"];

/** Every input that changes the result set, before the blanks are dropped. */
export interface SearchParams {
  q: string;
  mode: SearchMode;
  person?: string;
  since?: string;
  /** Limit matches to copies present in one connected Gmail account. */
  accountId?: string;
  /** A folder. Not a ranking input, so results stay in time order. */
  label?: string;
  /** Chains whose newest message is at or before this instant. A full timestamp so
   *  a page boundary inside a day neither repeats nor drops threads. */
  before?: string;
  limit?: number;
  entries?: boolean;
}

// Blank optionals are dropped: the server treats `person=` as a filter on the
// empty name, and dropping keeps them out of the query key.
export function searchQuery(p: SearchParams) {
  return {
    q: p.q,
    mode: p.mode,
    ...(p.person ? { person: p.person } : {}),
    ...(p.since ? { since: p.since } : {}),
    ...(p.accountId ? { accountId: p.accountId } : {}),
    ...(p.label ? { label: p.label } : {}),
    ...(p.before ? { before: p.before } : {}),
    ...(p.limit ? { limit: p.limit } : {}),
    ...(p.entries ? { entries: true } : {}),
  };
}
