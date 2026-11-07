import { useEffect, useId, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { fetchClient } from "../../lib/api/api";
import type { DraftSelection } from "./DraftSelectionContext";
import { draftIdentity } from "../../lib/ui/draftSelection";

/** Admission evidence for one opening, never permission to skip the actual save guards. */
export default function useDraftPreflight({
  kind,
  target,
  account,
  id,
  selection,
  applicable,
}: {
  kind: "compose" | "reply";
  target?: string;
  account: string;
  id: string;
  selection?: DraftSelection;
  applicable: boolean;
}) {
  const instance = useId();
  const ready = selection?.draft;
  const owned = id ? draftIdentity(account, id) : undefined;
  const opening = Boolean(selection?.active && applicable && !selection.adopting);
  const incoming =
    opening && ready
      ? JSON.stringify([selection?.epoch, ready.accountId, ready.id, kind, target])
      : "";
  const snapshot = useRef<
    | { identity: string; owner: string; accountId: string; id: string; messageId: string }
    | undefined
  >(undefined);
  const ownedSnapshot = useRef<typeof snapshot.current>(undefined);
  if (!owned) ownedSnapshot.current = undefined;
  else if (snapshot.current?.owner === owned) ownedSnapshot.current = snapshot.current;
  if (!selection?.active && !owned) snapshot.current = undefined;
  if (incoming && ready && snapshot.current?.identity !== incoming)
    snapshot.current = {
      identity: incoming,
      owner: draftIdentity(ready.accountId, ready.id),
      accountId: ready.accountId,
      id: ready.id,
      messageId: ready.messageId ?? "",
    };
  // Clearing a URL or adopting the same buffer must not erase a failed/pending admission.
  // Proven own-card adoption without an owned checkpoint belongs to an app-created draft.
  const retained = Boolean(owned && ownedSnapshot.current?.owner === owned);
  const current =
    incoming && snapshot.current?.identity === incoming
      ? snapshot.current
      : retained
        ? ownedSnapshot.current
        : undefined;
  const required = Boolean(current);
  const identity = current?.identity ?? "";
  const [admitted, setAdmitted] = useState<Set<string>>(() => new Set());
  const check = useQuery({
    queryKey: [
      "draft-save-preflight",
      instance,
      identity,
      current?.accountId,
      current?.id,
      current?.messageId,
    ],
    queryFn: async ({ signal }) => {
      if (!current?.messageId) throw new Error("Draft revision evidence unavailable");
      const { data, error } = await fetchClient.GET("/v1/drafts/{id}/save-check", {
        params: {
          path: { id: current.id },
          query: { accountId: current.accountId, expectedMessageId: current.messageId },
        },
        signal,
        cache: "no-store",
      });
      if (error || !data) throw new Error("Draft compatibility could not be checked");
      return data;
    },
    enabled: Boolean(required && current?.messageId && !admitted.has(identity)),
    retry: false,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    placeholderData: undefined,
  });
  const verified = Boolean(
    required &&
    current &&
    !check.isPlaceholderData &&
    check.data?.status === "supported" &&
    check.data.context === kind &&
    check.data.unchangedCompatible &&
    check.data.bodyEditCompatible,
  );
  useEffect(() => {
    if (verified)
      setAdmitted((previous) =>
        previous.has(identity) ? previous : new Set(previous).add(identity),
      );
  }, [verified, identity]);
  const blocked = required && !admitted.has(identity) && !verified;
  const ownedBlocked = Boolean(
    retained &&
    ownedSnapshot.current &&
    !admitted.has(ownedSnapshot.current.identity) &&
    !(verified && identity === ownedSnapshot.current.identity),
  );
  const pending =
    blocked &&
    Boolean(current?.messageId) &&
    !check.isError &&
    (check.isPending || check.isFetching);
  const failure = useRef<{ identity: string; message: string } | undefined>(undefined);
  let message =
    blocked && failure.current?.identity === identity ? failure.current.message : undefined;
  if (!blocked) failure.current = undefined;
  if (blocked && !pending) {
    if (!current?.messageId)
      message = "This draft has no revision evidence. Cancel and reopen it before editing.";
    else if (check.data?.status === "revision_changed")
      message =
        "This draft changed in Gmail. Cancel and reopen it to load the latest version before editing.";
    else if (check.isError)
      message =
        "Draft compatibility could not be checked. Retry the check, or edit this draft in Gmail.";
    else
      message =
        "Chainmail could not confirm this draft can be saved safely. Retry the check, or edit it in Gmail.";
    failure.current = { identity, message };
  }
  return {
    blocked,
    ownedBlocked,
    pending,
    message,
    retrying: check.isFetching,
    canRetry: Boolean(current?.messageId && check.data?.status !== "revision_changed"),
    retry: () => {
      void check.refetch();
    },
  };
}
