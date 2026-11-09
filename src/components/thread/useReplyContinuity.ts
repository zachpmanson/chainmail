import { useEffect, useRef } from "react";
import type { CorpusEntry } from "../../lib/api/api";
import { draftIdentity } from "../../lib/ui/draftSelection";
import type { DraftCoordinator } from "../compose/DraftSelectionContext";

/** Retain only an admitted editor's own buffer, never old scoped list data. */
export default function useReplyContinuity(
  answer: CorpusEntry | undefined,
  threadId: string,
  accountId: string | undefined,
  coordinator: DraftCoordinator | null,
) {
  const admitted = useRef<{
    answer: CorpusEntry;
    accountId: string | undefined;
    threadId: string;
    editor: DraftCoordinator["editor"]["current"];
    key: string;
  }>();
  const scope = useRef({ threadId, accountId, generation: 0 });
  const prior = admitted.current;
  const snapshot = coordinator?.editor.current?.();
  const owned =
    accountId &&
    prior?.threadId === threadId &&
    prior.editor === coordinator?.editor.current &&
    snapshot?.kind === "reply" &&
    snapshot.destination === threadId &&
    snapshot.target === prior.answer.extId &&
    (!coordinator?.selection.active ||
      coordinator.selection.adopting?.target === prior.answer.extId) &&
    snapshot.continuityAccountId === accountId &&
    (!answer || answer.extId === snapshot.target) &&
    (snapshot.transferAccountId === accountId ||
      (coordinator?.selection.adopting &&
        coordinator.selection.id &&
        snapshot.owner === draftIdentity(accountId, coordinator.selection.id)));
  const transition = scope.current.accountId !== accountId || scope.current.threadId !== threadId;
  // Never reuse an earlier scope's identity, including after an admitted From transfer.
  if (transition) {
    scope.current = {
      threadId,
      accountId,
      generation: scope.current.generation + (owned ? 0 : 1),
    };
  }
  const key = `reply:${scope.current.generation}`;
  const retained = !answer && owned ? prior?.answer : undefined;
  useEffect(() => {
    if (answer)
      admitted.current = { answer, accountId, threadId, editor: coordinator?.editor.current, key };
  }, [answer, accountId, threadId, coordinator?.editor, key]);
  return { answer: answer ?? retained, scopePending: Boolean(retained), key };
}
