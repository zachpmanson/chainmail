import { createContext, useContext, type MutableRefObject } from "react";
import type { SavedDraft } from "../../lib/ui/draftSelection";

export type PopoutLocation = {
  root: string;
  /** Empty only for a reader-only view with no mailbox scope. Replies require an explicit ID. */
  account: string;
  mode: "reply" | "reader";
  id?: string;
  messageId?: string;
  target?: string;
};
export type PreparedPopout = { location: PopoutLocation; draft?: SavedDraft };
export type ThreadPopouts = {
  frozen: boolean;
  hidden: boolean;
  managed: boolean;
  scope?: { root: string; account: string; token: string };
  reader: MutableRefObject<PopoutLocation | undefined>;
  prepared?: PreparedPopout;
  initialTarget?: PopoutLocation;
  away: (root: string, account?: string) => boolean;
  start: (root: string, account?: string) => void;
  focus: (root: string, account?: string) => void;
  recover: (root: string, account?: string) => void;
  returnToMain?: () => void;
  message: string;
};
export const ThreadPopoutContext = createContext<ThreadPopouts | null>(null);
export const useThreadPopouts = () => useContext(ThreadPopoutContext);
