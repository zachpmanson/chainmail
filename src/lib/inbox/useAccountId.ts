import { createContext, useContext } from "react";
import { useSearch } from "@tanstack/react-router";

/** Inbox supplies its settings-resolved scope; other pages use explicit URL scope. */
export const MailboxScope = createContext<{ accountId: string | undefined } | null>(null);

export function useAccountId(): string | undefined {
  const scope = useContext(MailboxScope);
  const routeAccountId = useSearch({ strict: false }).accountId;
  return scope ? scope.accountId : routeAccountId;
}
