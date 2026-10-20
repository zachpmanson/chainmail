import { useQueryClient } from "@tanstack/react-query";
import {
  $api,
  type MailActionRequest,
  type MailActionResponse,
  type MarkReadRequest,
  type MarkReadResponse,
} from "../api/api";
import { invalidateChains, invalidateSearch } from "../api/queryKeys";
import { dropFromLists, markInLists, putBackLists, type Was } from "./lists";

/** Reconcile every view whose mail-derived data changes after a mailbox write. */
export function staleAfterMail(queryClient: ReturnType<typeof useQueryClient>): void {
  void invalidateSearch(queryClient);
  void queryClient.invalidateQueries({ queryKey: ["get", "/v1/labels"] });
  void queryClient.invalidateQueries({ queryKey: ["get", "/v1/stats"] });
  void invalidateChains(queryClient);
}

const activeWrites = new WeakMap<ReturnType<typeof useQueryClient>, number>();

/** Count lifecycle callbacks, not mutation statuses: several writes can settle in one turn. */
export function mailWriteOptions<Variables>(
  queryClient: ReturnType<typeof useQueryClient>,
  patch: (variables: Variables) => Was,
) {
  return {
    onMutate: (variables: Variables) => {
      activeWrites.set(queryClient, (activeWrites.get(queryClient) ?? 0) + 1);
      return { was: patch(variables) };
    },
    onSettled: () => {
      const remaining = (activeWrites.get(queryClient) ?? 1) - 1;
      if (remaining > 0) {
        activeWrites.set(queryClient, remaining);
      } else {
        activeWrites.delete(queryClient);
        staleAfterMail(queryClient);
      }
    },
  };
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
    ...mailWriteOptions(queryClient, (variables: { body: MailActionRequest }) =>
      dropFromLists(queryClient, variables.body.chains),
    ),
    onSuccess: (response, variables) => {
      onSuccess?.(response, variables.body);
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
    ...mailWriteOptions(queryClient, (variables: { body: MarkReadRequest }) =>
      markInLists(queryClient, variables.body.chain, variables.body.unread),
    ),
    onSuccess: (response, variables) => {
      if (invalidateChain) void invalidateChains(queryClient);
      onSuccess?.(response, variables.body);
    },
    onError: (error, variables, context) => {
      if (context) putBackLists(queryClient, context.was);
      onError?.(error, variables.body);
    },
  });
}
