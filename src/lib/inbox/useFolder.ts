import { useNavigate, useSearch } from "@tanstack/react-router";
import { $api } from "../api/api";
import { COMPOSE_OPEN } from "../ui/openSelection";

/** The folder the inbox shows: the URL's, or the reader's default from settings. */
export default function useFolder() {
  const navigate = useNavigate();
  const { label: urlLabel, accountId: urlAccountId, open } = useSearch({ from: "/" });
  const settings = $api.useQuery("get", "/v1/settings", {});

  // "" in the URL is All mail chosen on purpose; only undefined falls back to the default.
  const home = urlLabel === undefined && urlAccountId === undefined;
  const label = urlLabel !== undefined ? urlLabel : (settings.data?.defaultFolder ?? "");
  const accountId =
    urlAccountId !== undefined
      ? urlAccountId
      : home
        ? settings.data?.defaultFolderAccountId
        : undefined;
  return {
    label,
    accountId,
    // Settled, not succeeded: a failed settings read means All mail, not waiting forever.
    settled: !settings.isPending,
    pick: (name: string, pickedAccountId?: string) =>
      navigate({
        to: "/",
        search: (prev) => ({
          ...prev,
          label: name,
          accountId: pickedAccountId,
          draft: prev.accountId === pickedAccountId ? prev.draft : undefined,
        }),
        state: (prev) => (open === COMPOSE_OPEN ? { composeOrigin: prev.composeOrigin } : {}),
      }),
  };
}
