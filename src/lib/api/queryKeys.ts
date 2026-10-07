import type { QueryClient } from "@tanstack/react-query";

// Prefixes of the keys openapi-react-query builds: they match every params variant.
export const chainsKey = ["get", "/v1/chains/{rootExtId}"] as const;
export const searchKey = ["get", "/v1/search"] as const;
export const opsPlanKey = ["get", "/v1/ops/plan"] as const;

export const invalidateChains = (qc: QueryClient) => qc.invalidateQueries({ queryKey: chainsKey });
export const invalidateSearch = (qc: QueryClient) => qc.invalidateQueries({ queryKey: searchKey });
export const invalidateOpsPlan = (qc: QueryClient) =>
  qc.invalidateQueries({ queryKey: opsPlanKey });
