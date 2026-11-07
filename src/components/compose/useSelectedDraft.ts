import { useEffect, useRef, useState } from "react";
import useDraftPreflight from "./useDraftPreflight";
import { draftIdentity, type SavedDraft } from "../../lib/ui/draftSelection";
import { useDraftCoordinator, type LeaveSnapshot } from "./DraftSelectionContext";

/** Selection admission is separate from the existing serialized write queue. */
export default function useSelectedDraft({
  kind,
  account,
  id,
  target,
  hydrate,
  snapshot,
}: {
  kind: "compose" | "reply";
  account: string;
  id: string;
  target?: string;
  hydrate: (draft: SavedDraft) => void;
  snapshot: () => LeaveSnapshot;
}) {
  const coordinator = useDraftCoordinator();
  const selection = coordinator?.selection;
  const edits = useRef(0);
  const pending = useRef({ epoch: selection?.epoch, edits: edits.current });
  const [appliedEpoch, setAppliedEpoch] = useState<number>();
  const latest = useRef({ hydrate, snapshot });
  latest.current = { hydrate, snapshot };
  const [replacement, setReplacement] = useState(false);
  if (pending.current.epoch !== selection?.epoch)
    pending.current = { epoch: selection?.epoch, edits: edits.current };
  const editor = coordinator?.editor;
  useEffect(() => {
    if (!editor) return;
    const get = () => {
      const snapshot = latest.current.snapshot();
      return {
        ...snapshot,
        revision: JSON.stringify([edits.current, snapshot.revision]),
        invalidate: () => {
          pending.current.edits = edits.current;
          snapshot.invalidate();
        },
      };
    };
    editor.current = get;
    return () => {
      if (editor.current === get) editor.current = undefined;
    };
  }, [editor]);
  const ready = selection?.draft;
  const applicable =
    ready?.kind === kind &&
    (kind !== "reply" || (ready.accountId === account && ready.replyToEntry === target));
  useEffect(() => {
    if (!selection?.active || !applicable || !ready || appliedEpoch === selection.epoch) return;
    if (ready.id === id && ready.accountId === account) {
      setAppliedEpoch(selection.epoch);
      setReplacement(false);
      return;
    }
    if (edits.current !== pending.current.edits) {
      setReplacement(true);
      return;
    }
    latest.current.hydrate(ready);
    setAppliedEpoch(selection.epoch);
    setReplacement(false);
  }, [selection?.epoch, selection?.active, applicable, ready, id, account, appliedEpoch]);
  const preflight = useDraftPreflight({ kind, target, account, id, selection, applicable });
  const blocked =
    preflight.blocked ||
    Boolean(selection?.active && (!applicable || appliedEpoch !== selection.epoch));
  return {
    coordinator,
    blocked,
    preflight,
    editingBlocked:
      preflight.ownedBlocked ||
      (preflight.blocked && (!selection?.active || appliedEpoch === selection.epoch)),
    replacement: replacement && blocked,
    edit: () => {
      edits.current++;
    },
    replace: () => {
      if (!selection || !ready || !applicable || !coordinator?.approve()) return;
      latest.current.hydrate(ready);
      setAppliedEpoch(selection.epoch);
      setReplacement(false);
    },
    owner: id ? draftIdentity(account, id) : undefined,
  };
}
