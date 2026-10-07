import { $api } from "../../lib/api/api";

export default function useAccounts() {
  const accounts = $api.useQuery("get", "/auth/status", {});
  const all = accounts.data?.accounts ?? [];
  return {
    connected: all.filter((account) => account.signedIn),
    nameOf: (id: string | undefined) => all.find((account) => account.id === id)?.displayName ?? id,
  };
}
