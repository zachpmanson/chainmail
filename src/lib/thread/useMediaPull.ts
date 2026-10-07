import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError, $api } from "../api/api";
import { pullSummary } from "../message/attachments";

export function useMediaPull(accountId: string | undefined): {
  pulling: string | null;
  pullNote: string | null;
  pull: (extId: string) => void;
} {
  const queryClient = useQueryClient();
  // One pull at a time; the other buttons are held while one is out.
  const [pulling, setPulling] = useState<string | null>(null);
  const [pullNote, setPullNote] = useState<string | null>(null);

  // No spec to patch: re-read the thread, and release the button only once that lands.
  const mutation = $api.useMutation("post", "/v1/media/pull", {
    onSuccess: (data) => {
      console.log(`fetch: ${pullSummary(data)}`);
      setPullNote(null);
      void queryClient
        .invalidateQueries({ queryKey: ["get", "/v1/chains/{rootExtId}"] })
        .finally(() => setPulling(null));
    },
    onError: (e) => {
      setPulling(null);
      setPullNote(
        e instanceof ApiError && e.status === 403
          ? "This host cannot fetch files (it was started without -media)."
          : `Fetching the files failed: ${e instanceof Error ? e.message : String(e)}. Nothing was stored — press again to retry.`,
      );
    },
  });

  const pull = (extId: string) => {
    setPulling(extId);
    setPullNote(null);
    mutation.mutate({ body: { entry: extId, ...(accountId ? { accountId } : {}) } });
  };

  return { pulling, pullNote, pull };
}
