import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError, $api, type MediaPull } from "../api/api";
import { invalidateChains } from "../api/queryKeys";
import { pullSummary } from "../message/attachments";
import { errText } from "../ui/errText";
import { pushToast, SAID_MS } from "../ui/toasts";

export function useMediaPull({
  accountId,
  name,
  onPulled,
}: {
  accountId?: string;
  /** A saved page's name: the server then rebuilds and saves that page itself. */
  name?: string;
  /** The button is held until this settles. Defaults to re-reading the thread. */
  onPulled?: (data: MediaPull) => Promise<unknown> | void;
}): {
  pulling: string | null;
  pullNote: string | null;
  pull: (extId: string) => void;
} {
  const queryClient = useQueryClient();
  // One pull at a time; the other buttons are held while one is out.
  const [pulling, setPulling] = useState<string | null>(null);
  const [pullNote, setPullNote] = useState<string | null>(null);

  const mutation = $api.useMutation("post", "/v1/media/pull", {
    onSuccess: (data) => {
      pushToast(`Attachments: ${pullSummary(data)}`, "note", SAID_MS);
      setPullNote(null);
      const after = onPulled ? onPulled(data) : invalidateChains(queryClient);
      void Promise.resolve(after).finally(() => setPulling(null));
    },
    onError: (e) => {
      setPulling(null);
      setPullNote(
        e instanceof ApiError && e.status === 403
          ? "This host cannot fetch files (it was started without -media)."
          : `Fetching the files failed: ${errText(e)}. Nothing was stored — press again to retry.`,
      );
    },
  });

  const pull = (extId: string) => {
    setPulling(extId);
    setPullNote(null);
    mutation.mutate({
      body: { entry: extId, ...(name ? { name } : {}), ...(accountId ? { accountId } : {}) },
    });
  };

  return { pulling, pullNote, pull };
}
