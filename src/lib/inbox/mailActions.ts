import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  ApiError,
  fetchClient,
  type MailActionRequest,
  type MailActionResponse,
  type MarkReadRequest,
  type MarkReadResponse,
} from "../api/api";
import { invalidateChains, invalidateSearch } from "../api/queryKeys";
import { dropFromLists, markInLists, putBackLists, type Was } from "./lists";
import { appUndo } from "../ui/undo";
import { pushToast } from "../ui/toasts";

function rememberMail(
  token: string | undefined,
  label: string,
  queryClient: ReturnType<typeof useQueryClient>,
) {
  if (!token) return;
  appUndo.remember({
    label,
    undo: async () => {
      try {
        await fetchClient.POST("/v1/mail/undo", { body: { token } });
      } finally {
        staleAfterMail(queryClient);
      }
    },
  });
}

async function undoableMail<T extends { undoToken?: string }>(
  action: () => Promise<T>,
  label: string,
  queryClient: ReturnType<typeof useQueryClient>,
): Promise<T> {
  return appUndo.perform(async () => {
    try {
      const response = await action();
      rememberMail(response.undoToken, label, queryClient);
      return response;
    } catch (error) {
      if (error instanceof ApiError && error.undoToken) {
        rememberMail(error.undoToken, `partial ${label}`, queryClient);
        pushToast(
          "Some messages changed. Undo can restore confirmed changes only; check Gmail for the failed message.",
          "fail",
        );
      }
      throw error;
    }
  });
}

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
  return useMutation({
    mutationFn: (variables: { body: MailActionRequest }) =>
      undoableMail(
        async () => {
          const { data } = await fetchClient.POST("/v1/mail", {
            body: { ...variables.body, undoable: true },
          });
          return data!;
        },
        variables.body.action,
        queryClient,
      ),
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
  return useMutation({
    mutationFn: async (variables: { body: MarkReadRequest }) => {
      const { data } = await fetchClient.POST("/v1/read", {
        body: { ...variables.body, undoable: false },
      });
      return data!;
    },
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
