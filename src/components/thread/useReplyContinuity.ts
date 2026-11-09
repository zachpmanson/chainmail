import { useEffect, useRef } from "react";
import type { CorpusEntry } from "../../lib/api/api";
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
    threadId: string;
    editor: DraftCoordinator["editor"]["current"];
  }>();
  useEffect(() => {
    if (answer) admitted.current = { answer, threadId, editor: coordinator?.editor.current };
  }, [answer, threadId, coordinator?.editor]);
  const prior = admitted.current;
  const snapshot = coordinator?.editor.current?.();
  const retained =
    !answer &&
    accountId &&
    prior?.threadId === threadId &&
    prior.editor === coordinator?.editor.current &&
    snapshot?.kind === "reply" &&
    snapshot.destination === threadId &&
    snapshot.target === prior.answer.extId &&
    (!coordinator?.selection.active ||
      coordinator.selection.adopting?.target === prior.answer.extId) &&
    snapshot.continuityAccountId === accountId
      ? prior.answer
      : undefined;
  return { answer: answer ?? retained, scopePending: Boolean(retained) };
}
