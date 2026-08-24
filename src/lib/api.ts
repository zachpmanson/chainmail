import createFetchClient, { type Middleware } from "openapi-fetch";
import createQueryClient from "openapi-react-query";
import type { components, paths } from "./api.d";

/**
 * A response the service declined with. The message is the server's own, because
 * the failures here are informative: "no embedding daemon", "unknown ext id",
 * "unparseable query". Replacing them with "Request failed" would throw away the
 * only part a person can act on.
 */
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

/**
 * Retry-After, as seconds or as an HTTP date. Absent or unparseable yields
 * undefined rather than a guess: waiting an invented interval on a service that
 * told you nothing is how a client turns a queue into a stampede.
 */
function retryAfter(response: Response): number | undefined {
  const raw = response.headers.get("retry-after");
  if (raw === null) return undefined;
  const seconds = Number(raw.trim());
  if (Number.isFinite(seconds) && seconds >= 0) return seconds * 1000;
  const at = Date.parse(raw);
  if (Number.isNaN(at)) return undefined;
  return Math.max(0, at - Date.now());
}

/**
 * True when the failure is the service's answer rather than a hiccup on the way
 * to it. Retrying an answer cannot change it: a 404 for an unknown ext id, a 400
 * for a bad query, a 503 for a stopped embedding daemon and a 504 for a missed
 * embedding deadline are all stable facts, and re-asking only delays saying so.
 *
 * 429 is the exception, and the only status worth retrying: the service is
 * holding two spec builds already and says when to come back, so the answer is
 * "not yet" rather than "no".
 */
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

/**
 * A decline becomes a thrown ApiError here rather than a returned `{error}`,
 * because the retry policy and every message on screen are decided from the
 * status and Retry-After. openapi-react-query rethrows whatever the fetch client
 * returns as `error` — which is the parsed body alone, with the status and
 * headers already discarded — so classifying any later than this would mean
 * classifying without the two facts the decision needs.
 *
 * The body is consumed to build the message, which is safe only because this
 * throws: no caller downstream is left a Response it can still read.
 */
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

/**
 * Same origin: the dev server proxies /v1 to the service, so no request here is
 * cross-origin and the service needs no CORS allowance widened to serve it.
 *
 * fetch is looked up per call rather than captured, so a test that replaces the
 * global is observed by a client built at import time.
 */
const fetchClient = createFetchClient<paths>({
  // The page's own origin, spelled out: the joined path becomes a Request, and
  // outside a browser that constructor rejects a relative URL. Under the dev
  // server this origin is the one whose /v1 is proxied to the service.
  baseUrl: typeof location === "undefined" ? "http://127.0.0.1/" : location.origin,
  fetch: (request) => globalThis.fetch(request),
});
fetchClient.use(declineAsError);

/**
 * The hooks, keyed by method, path and the init object — so every parameter that
 * changes the result set is in the key by construction, mode included.
 *
 * That is the whole reason to hold the binding rather than hand-written hooks: a
 * key that omits a parameter is a cache hit across a change the server would
 * have answered differently, so a mode switch would leave the lexical results on
 * screen while the semantic request runs and the page would assert an answer the
 * service never gave. Nothing here sets placeholderData, so a key with no cached
 * entry shows the pending state instead.
 */
export const $api = createQueryClient(fetchClient);

export type SearchMode = "lexical" | "semantic" | "hybrid";

export type ChainHit = components["schemas"]["ChainHit"];

export type CorpusEntry = components["schemas"]["CorpusEntry"];

export type ServiceStatus = components["schemas"]["ServiceStatus"];

export type StatusResponse = components["schemas"]["StatusResponse"];

export type Stats = components["schemas"]["Stats"];

export type RefreshReport = components["schemas"]["RefreshReport"];

export type RefreshCandidate = components["schemas"]["RefreshCandidate"];

/** Every input that changes the result set, before the blanks are dropped. */
export interface SearchParams {
  q: string;
  mode: SearchMode;
  person?: string;
  since?: string;
  limit?: number;
  entries?: boolean;
}

/**
 * Blank optionals are dropped rather than sent empty: `person=` is a filter on
 * the empty name to a server that takes the parameter literally, which is a
 * different search from not filtering at all. Dropping them also keeps them out
 * of the derived key, so filling a filter and clearing it again is the same
 * search it started as.
 */
export function searchQuery(p: SearchParams) {
  return {
    q: p.q,
    mode: p.mode,
    ...(p.person ? { person: p.person } : {}),
    ...(p.since ? { since: p.since } : {}),
    ...(p.limit ? { limit: p.limit } : {}),
    ...(p.entries ? { entries: true } : {}),
  };
}
