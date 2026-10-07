import { useQueryClient } from "@tanstack/react-query";
import {
  $api,
  type MailActionRequest,
  type MailActionResponse,
  type MarkReadRequest,
  type MarkReadResponse,
} from "../api/api";
import { invalidateChains, invalidateSearch } from "../api/queryKeys";
import { dropFromLists, markInLists, putBackLists } from "./lists";

/** Reconcile every view whose mail-derived data changes after a mailbox write. */
export function staleAfterMail(queryClient: ReturnType<typeof useQueryClient>): void {
  void invalidateSearch(queryClient);
  void queryClient.invalidateQueries({ queryKey: ["get", "/v1/labels"] });
  void queryClient.invalidateQueries({ queryKey: ["get", "/v1/stats"] });
  void invalidateChains(queryClient);
}

/** Surface-owned feedback remains a caller concern; this hook owns shared cache/write behavior. */
export function useMailAction({
  onSuccess,
  onError,
}: {
  onSuccess?: (response: MailActionResponse, request: MailActionRequest) => void;
  onError?: (error: unknown, request: MailActionRequest) => void;
} = {}) {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/v1/mail", {
    onMutate: (variables) => ({ was: dropFromLists(queryClient, variables.body.chains) }),
    onSuccess: (response, variables) => {
      onSuccess?.(response, variables.body);
      staleAfterMail(queryClient);
    },
    onError: (error, variables, context) => {
      if (context) putBackLists(queryClient, context.was);
      onError?.(error, variables.body);
    },
  });
}

/** Keep unread-list optimism and rollback consistent wherever read state is toggled. */
export function useReadAction({
  invalidateChain = false,
  onSuccess,
  onError,
}: {
  /** The reading pane also needs to refresh the open chain; list rows do not. */
  invalidateChain?: boolean;
  onSuccess?: (response: MarkReadResponse, request: MarkReadRequest) => void;
  onError?: (error: unknown, request: MarkReadRequest) => void;
} = {}) {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/v1/read", {
    onMutate: (variables) => ({
      was: markInLists(queryClient, variables.body.chain, variables.body.unread),
    }),
    onSuccess: (response, variables) => {
      void invalidateSearch(queryClient);
      if (invalidateChain) void invalidateChains(queryClient);
      onSuccess?.(response, variables.body);
    },
    onError: (error, variables, context) => {
      if (context) putBackLists(queryClient, context.was);
      onError?.(error, variables.body);
    },
  });
}
