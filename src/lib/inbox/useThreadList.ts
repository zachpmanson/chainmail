import { $api } from "../api/api";
import { nextCursor, PAGE, uniqueChains } from "./threadPages";

/** One folder's threads, newest first, a page at a time. */
export default function useThreadList(
  label: string,
  accountId: string | undefined,
  enabled: boolean,
) {
  // pageParamName injects the cursor as `before`.
  const list = $api.useInfiniteQuery(
    "get",
    "/v1/search",
    {
      params: {
        query: { limit: PAGE, ...(label ? { label } : {}), ...(accountId ? { accountId } : {}) },
      },
    },
    {
      enabled,
      pageParamName: "before",
      initialPageParam: "",
      getNextPageParam: nextCursor,
    },
  );
  return { list, rows: uniqueChains(list.data?.pages ?? []) };
}
