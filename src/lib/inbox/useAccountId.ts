import { useSearch } from "@tanstack/react-router";

/** The Gmail account the inbox URL is scoped to, if any. */
export function useAccountId(): string | undefined {
  return useSearch({ from: "/" }).accountId;
}
