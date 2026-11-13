import { useState } from "react";
import { IconRefresh } from "@tabler/icons-react";
import { useIsFetching, useIsMutating, useQueryClient } from "@tanstack/react-query";
import { hassleMutation, nativeOperations } from "../../lib/hassles/native";
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
  const writing = useIsMutating({ predicate: hassleMutation });
  const refresh = async () => {
    setBusy(true);
    try {
      const config = await qc.fetchQuery(
        $api.queryOptions("get", "/v1/hassles/location", {}, { staleTime: 0, retry: false }),
      );
      const store = nativeOperations(qc);
      await store.status();
      await store.refreshOperation();
      if (config.enabled) await store.inventory(config.location, true);
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
      <IconRefresh
        aria-hidden="true"
        className={busy || fetching > 0 ? "motion-safe:animate-spin" : ""}
      />
    </IconButton>
  );
}
