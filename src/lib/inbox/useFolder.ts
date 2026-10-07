import { useNavigate, useSearch } from "@tanstack/react-router";
import { $api } from "../api/api";
import { useAccountId } from "./useAccountId";

/** The folder the inbox shows: the URL's, or the reader's default from settings. */
export default function useFolder() {
  const navigate = useNavigate();
  const urlLabel = useSearch({ from: "/" }).label;
  const urlAccountId = useAccountId();
  const settings = $api.useQuery("get", "/v1/settings", {});
  const save = $api.useMutation("post", "/v1/settings", {
    onSuccess: () => {
      void settings.refetch();
    },
  });

  // "" in the URL is All mail chosen on purpose; only undefined falls back to the default.
  const home = urlLabel === undefined && urlAccountId === undefined;
  const label = urlLabel !== undefined ? urlLabel : (settings.data?.defaultFolder ?? "");
  const accountId =
    urlAccountId !== undefined
      ? urlAccountId
      : home
        ? settings.data?.defaultFolderAccountId
        : undefined;
  // Compare the whole location: one folder name can exist in several accounts.
  const isDefault =
    (settings.data?.defaultFolder ?? "") === label &&
    (settings.data?.defaultFolderAccountId ?? "") === (accountId ?? "");

  return {
    label,
    accountId,
    isDefault,
    // Settled, not succeeded: a failed settings read means All mail, not waiting forever.
    settled: !settings.isPending,
    pick: (name: string, pickedAccountId?: string) =>
      navigate({
        to: "/",
        search: (prev) => ({ ...prev, label: name, accountId: pickedAccountId }),
      }),
    makeDefault: (on: boolean) =>
      save.mutate({
        body: {
          defaultFolder: on ? label : "",
          defaultFolderAccountId: on ? (accountId ?? "") : "",
        },
      }),
  };
}
