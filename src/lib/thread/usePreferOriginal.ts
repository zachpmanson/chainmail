import { useQueryClient } from "@tanstack/react-query";
import { $api, type CorpusEntry } from "../api/api";
import { chainsKey, invalidateChains } from "../api/queryKeys";
import type { ReadingPreference } from "../prefs/usePrefs";
import { errText } from "../ui/errText";
import { pushToast } from "../ui/toasts";

/** Optimistic: patch every cached chain for this person, then invalidate; roll back on error. */
export function usePreferOriginal() {
  const queryClient = useQueryClient();
  const prefer = $api.useMutation("post", "/v1/people/{personId}", {
    onMutate: async ({ params, body }) => {
      await queryClient.cancelQueries({ queryKey: chainsKey });
      const previous = queryClient.getQueriesData<{ entries: CorpusEntry[] }>({
        queryKey: chainsKey,
      });
      queryClient.setQueriesData<{ entries: CorpusEntry[] }>(
        { queryKey: chainsKey },
        (old) =>
          old && {
            ...old,
            entries: old.entries.map((e) =>
              e.personId === params.path.personId
                ? {
                    ...e,
                    preferOriginal: body.preferOriginal === true,
                    preferOriginalSet: body.resetOriginalPreference !== true,
                  }
                : e,
            ),
          },
      );
      return previous;
    },
    onError: (err, { params }, previous) => {
      for (const [key, data] of previous ?? []) {
        queryClient.setQueryData<{ entries: CorpusEntry[] }>(
          key,
          (current) =>
            current && {
              ...current,
              entries: current.entries.map((e) => {
                const was = data?.entries.find((entry) => entry.extId === e.extId);
                return e.personId === params.path.personId && was
                  ? {
                      ...e,
                      preferOriginal: was.preferOriginal,
                      preferOriginalSet: was.preferOriginalSet,
                    }
                  : e;
              }),
            },
        );
      }
      pushToast(
        `That reading style did not stick: ${errText(err)}. ` +
          "Nothing was stored — choose again to retry.",
        "fail",
      );
    },
    onSettled: () => {
      void invalidateChains(queryClient);
    },
  });

  return {
    pending: prefer.isPending,
    flip: async (personId: number, next: ReadingPreference) => {
      await prefer.mutateAsync({
        params: { path: { personId } },
        body: next === undefined ? { resetOriginalPreference: true } : { preferOriginal: next },
      });
    },
  };
}
