import { QueryClient } from "@tanstack/react-query";
import { ApiError, isAnswer } from "./api";

/**
 * A status the service returned is an answer, so it is shown rather than
 * re-asked. The two exceptions are a 429, where the service said "not yet" and
 * when to come back, and never reaching the service at all.
 */
function retry(attempt: number, err: Error): boolean {
  if (err instanceof ApiError && err.status === 429) return attempt < 2;
  return !isAnswer(err) && attempt < 1;
}

/** The delay the service named, or a second when it named none. */
function retryDelay(_attempt: number, err: Error): number {
  return err instanceof ApiError ? (err.retryAfterMs ?? 1000) : 1000;
}

/**
 * The retry policy is set here, on the client, rather than per hook: it is a
 * judgement about what this service's statuses mean, and it holds for every call
 * to it. A hook that opted out would be re-asking a question already answered.
 */
export function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry,
        retryDelay,
        // The corpus changes only when an operator ingests, which no view here
        // can observe; refetching on focus would re-pay a search for nothing.
        refetchOnWindowFocus: false,
        staleTime: 5 * 60 * 1000,
      },
      // A spec build is rate-limited to two in flight, so the one thing a
      // mutation retries is being told to wait.
      mutations: { retry, retryDelay },
    },
  });
}
