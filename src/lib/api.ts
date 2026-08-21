import createClient from "openapi-fetch";
import type { paths } from "./api.d";
import { normalise } from "./normalise";
import type { Timeline } from "./spec";

/**
 * A response the service declined with. The message is the server's own, because
 * the failures here are informative: "no embedding daemon", "unknown ext id",
 * "unparseable query". Replacing them with "Request failed" would throw away the
 * only part a person can act on.
 */
export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

/**
 * True when the failure is the service's answer rather than a hiccup on the way
 * to it. Retrying an answer cannot change it: a 404 for an unknown ext id, a 400
 * for a bad query and a 503 for a stopped embedding daemon are all stable facts,
 * and re-asking only delays telling the person about them.
 */
export function isAnswer(err: unknown): boolean {
  return err instanceof ApiError;
}

/**
 * Same origin: the dev server proxies /v1 to the service, so no request here is
 * cross-origin and the service needs no CORS allowance widened to serve it.
 *
 * fetch is looked up per call rather than captured, so a test that replaces the
 * global is observed by a client built at import time.
 */
const client = createClient<paths>({
  // The page's own origin, spelled out: the joined path becomes a Request, and
  // outside a browser that constructor rejects a relative URL. Under the dev
  // server this origin is the one whose /v1 is proxied to the service.
  baseUrl: typeof location === "undefined" ? "http://127.0.0.1/" : location.origin,
  fetch: (request) => globalThis.fetch(request),
});

/** The `{error: string}` body the service uses; falls back to whatever arrived. */
function messageOf(body: unknown, status: number, statusText: string): string {
  if (body && typeof body === "object" && "error" in body) {
    const e = (body as { error?: unknown }).error;
    if (typeof e === "string" && e.trim() !== "") return e;
  }
  if (typeof body === "string" && body.trim() !== "") return body.trim();
  return statusText.trim() !== "" ? `${status} ${statusText}` : `HTTP ${status}`;
}

function fail(error: unknown, response: Response): never {
  throw new ApiError(response.status, messageOf(error, response.status, response.statusText));
}

export type SearchMode = "lexical" | "semantic" | "hybrid";

/** Every input that changes the result set. Also the query key — see keys(). */
export interface SearchParams {
  q: string;
  mode: SearchMode;
  person?: string;
  since?: string;
  limit?: number;
  entries?: boolean;
}

export type ChainHit =
  NonNullable<paths["/v1/search"]["get"]["responses"]["200"]["content"]["application/json"]["chains"]>[number];
export type EntryHit =
  NonNullable<paths["/v1/search"]["get"]["responses"]["200"]["content"]["application/json"]["entries"]>[number];
export type Stats =
  paths["/v1/stats"]["get"]["responses"]["200"]["content"]["application/json"];
export type Person =
  paths["/v1/people"]["get"]["responses"]["200"]["content"]["application/json"][number];

export interface SearchResult {
  chains: ChainHit[];
  entries: EntryHit[];
}

/**
 * Blank optionals are dropped rather than sent empty: `person=` is a filter on
 * the empty name to a server that takes the parameter literally, which is a
 * different search from not filtering at all.
 */
function searchQuery(p: SearchParams) {
  return {
    q: p.q,
    mode: p.mode,
    ...(p.person ? { person: p.person } : {}),
    ...(p.since ? { since: p.since } : {}),
    ...(p.limit ? { limit: p.limit } : {}),
    ...(p.entries ? { entries: true } : {}),
  };
}

export async function search(p: SearchParams): Promise<SearchResult> {
  const { data, error, response } = await client.GET("/v1/search", {
    params: { query: searchQuery(p) },
  });
  if (error !== undefined || !data) fail(error, response);
  return { chains: data.chains ?? [], entries: data.entries ?? [] };
}

export interface SpecRequest {
  chains: string[];
  title?: string;
  me?: string;
}

/**
 * Builds a page from a chosen set. The spec is passed through normalise() for the
 * same reason a file-loaded one is: the renderer downstream is entitled to see
 * exactly one shape whatever produced it.
 */
export async function buildSpec(req: SpecRequest): Promise<Timeline> {
  const { data, error, response } = await client.POST("/v1/spec", { body: req });
  if (error !== undefined || !data) fail(error, response);
  return normalise(data as Record<string, unknown>);
}

export async function getEntry(extId: string): Promise<EntryHit> {
  const { data, error, response } = await client.GET("/v1/entries/{extId}", {
    params: { path: { extId } },
  });
  if (error !== undefined || !data) fail(error, response);
  return data;
}

export async function getStats(): Promise<Stats> {
  const { data, error, response } = await client.GET("/v1/stats", {});
  if (error !== undefined || !data) fail(error, response);
  return data;
}

export async function getPeople(): Promise<Person[]> {
  const { data, error, response } = await client.GET("/v1/people", {});
  if (error !== undefined || !data) fail(error, response);
  return data;
}
