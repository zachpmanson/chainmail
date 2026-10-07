import { useQueryClient } from "@tanstack/react-query";
import { $api, type CorpusEntry } from "../api/api";
import { chainsKey, invalidateChains } from "../api/queryKeys";
import { errText } from "../ui/errText";
import { pushToast } from "../ui/toasts";

/** Optimistic: patch every cached chain for this person, then invalidate; roll back on error. */
export function usePreferOriginal(): (personId: number, next: boolean) => void {
  const queryClient = useQueryClient();
  const prefer = $api.useMutation("post", "/v1/people/{personId}");

  return (personId, next) => {
    queryClient.setQueriesData<{ entries: CorpusEntry[] }>(
      { queryKey: chainsKey },
      (old) =>
        old && {
          ...old,
          entries: old.entries.map((e) =>
            e.personId === personId ? { ...e, preferOriginal: next } : e,
          ),
        },
    );
    prefer.mutate(
      { params: { path: { personId } }, body: { preferOriginal: next } },
      {
        onError: (err) => {
          pushToast(
            `That reading style did not stick: ${errText(err)}. ` +
              "Nothing was stored — press again to retry.",
            "fail",
          );
        },
        onSettled: () => {
          void invalidateChains(queryClient);
        },
      },
    );
  };
}
