import { QueryClient, useMutation, useQuery } from "@tanstack/react-query";
import {
  buildSpec,
  isAnswer,
  search,
  type SearchParams,
  type SearchResult,
  type SpecRequest,
} from "./api";
import type { Timeline } from "./spec";

/**
 * Query keys carry every parameter that changes the result, mode included.
 *
 * The alternative — keying on the text alone and passing mode as a closure —
 * makes a mode switch a cache hit: the lexical results stay on screen while the
 * semantic request runs, so the page asserts an answer the server never gave.
 * Because each parameter set is its own key and nothing here keeps previous
 * data, switching mode empties the list and shows the pending state instead.
 */
export const keys = {
  search: (p: SearchParams) =>
    [
      "search",
      {
        q: p.q,
        mode: p.mode,
        person: p.person ?? "",
        since: p.since ?? "",
        limit: p.limit ?? 0,
        entries: p.entries ?? false,
      },
    ] as const,
  entry: (extId: string) => ["entry", extId] as const,
  stats: () => ["stats"] as const,
  people: () => ["people"] as const,
};

export function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // A status the service returned is an answer, so it is shown, not
        // re-asked. Only a failure to reach the service at all is worth one
        // more attempt.
        retry: (attempt, err) => !isAnswer(err) && attempt < 1,
        // The corpus changes only when an operator ingests, which no view here
        // can observe; refetching on focus would re-pay a search for nothing.
        refetchOnWindowFocus: false,
        staleTime: 5 * 60 * 1000,
      },
      mutations: { retry: false },
    },
  });
}

/** Ranked chains for a submitted search. `params: null` means nothing asked yet. */
export function useSearch(params: SearchParams | null) {
  return useQuery<SearchResult>({
    queryKey: params ? keys.search(params) : ["search", "idle"],
    queryFn: () => search(params!),
    enabled: params !== null && params.q.trim() !== "",
  });
}

/**
 * Building a spec runs HTML recovery and boilerplate detection over the whole
 * selection, which takes seconds. A mutation, so it fires when a person asks for
 * a page and never as a side effect of ticking a box — a selection of twelve
 * chains would otherwise queue twelve builds and render the eleventh's answer.
 */
export function useSpecBuild(onBuilt: (spec: Timeline) => void) {
  return useMutation<Timeline, Error, SpecRequest>({
    mutationFn: buildSpec,
    onSuccess: onBuilt,
  });
}
