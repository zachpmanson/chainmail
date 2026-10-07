import { QueryClient } from "@tanstack/react-query";
import { ApiError, isAnswer } from "./api";

/** Only a 429 or a network failure is retried; any other status is the answer. */
function retry(attempt: number, err: Error): boolean {
  if (err instanceof ApiError && err.status === 429) return attempt < 2;
  return !isAnswer(err) && attempt < 1;
}

/** The delay the service named, or a second when it named none. */
function retryDelay(_attempt: number, err: Error): number {
  return err instanceof ApiError ? (err.retryAfterMs ?? 1000) : 1000;
}

export function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry,
        retryDelay,
        // AutoRefresh owns refetch-on-focus; a second focus policy here would race it.
        refetchOnWindowFocus: false,
        staleTime: 5 * 60 * 1000,
      },
      // Spec builds are rate-limited to two in flight; retry only on 429.
      mutations: { retry, retryDelay },
    },
  });
}
