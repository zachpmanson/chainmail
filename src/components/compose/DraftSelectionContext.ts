import { createContext, useContext, type MutableRefObject } from "react";
import type { SavedDraft } from "../../lib/ui/draftSelection";

export type LeaveSnapshot = {
  owner?: string;
  kind?: "compose" | "reply";
  destination?: string;
  target?: string;
  revision?: string;
  busy: boolean;
  dirty: boolean;
  invalidate: () => void;
};
export type DraftSelection = {
  epoch: number;
  accountId?: string;
  id?: string;
  active: boolean;
  draft?: SavedDraft;
  adopting?: { target?: string; destination: string };
};
export type DraftCoordinator = {
  selection: DraftSelection;
  editor: MutableRefObject<(() => LeaveSnapshot) | undefined>;
  select: (draft: SavedDraft) => void;
  detach: (account: string) => boolean;
  approve: () => boolean;
  retire: (account: string, id: string) => void;
  isRetired: (account: string, id: string) => boolean;
};
export const DraftSelectionContext = createContext<DraftCoordinator | null>(null);
export function useDraftCoordinator() {
  return useContext(DraftSelectionContext);
}
