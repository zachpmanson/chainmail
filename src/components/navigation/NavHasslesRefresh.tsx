import { useState } from "react";
import { ArrowPathIcon } from "@heroicons/react/24/outline";
import { useIsFetching, useIsMutating, useQueryClient } from "@tanstack/react-query";
import { $api } from "../../lib/api/api";
import { errText } from "../../lib/ui/errText";
import { pushToast } from "../../lib/ui/toasts";
import IconButton from "../ui/IconButton";

export default function NavHasslesRefresh() {
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const fetching = useIsFetching({
    predicate: (query) =>
      typeof query.queryKey[1] === "string" && query.queryKey[1].startsWith("/v1/hassles"),
  });
  const writing = useIsMutating({
    predicate: (mutation) =>
      mutation.options.mutationKey?.some(
        (key) => typeof key === "string" && key.startsWith("/v1/hassles"),
      ) ?? false,
  });
  const refresh = async () => {
    setBusy(true);
    try {
      const config = await qc.fetchQuery(
        $api.queryOptions("get", "/v1/hassles/location", {}, { staleTime: 0, retry: false }),
      );
      if (config.enabled && config.location.accountId && config.location.calendarId) {
        await qc.invalidateQueries(
          {
            exact: true,
            queryKey: $api.queryOptions("get", "/v1/hassles", {
              params: { query: config.location },
            }).queryKey,
          },
          { cancelRefetch: false, throwOnError: true },
        );
      }
    } catch (error) {
      pushToast(`Refreshing hassles failed: ${errText(error)}`, "fail");
    } finally {
      setBusy(false);
    }
  };
  return (
    <IconButton
      aria-label="Refresh hassles"
      title="Refresh hassles from Calendar"
      aria-busy={busy || fetching > 0}
      disabled={busy || fetching > 0 || writing > 0}
      onClick={() => void refresh()}
    >
      <ArrowPathIcon
        aria-hidden="true"
        className={busy || fetching > 0 ? "motion-safe:animate-spin" : ""}
      />
    </IconButton>
  );
}
