import { createContext, useContext } from "react";
import { useStore } from "zustand";
import type { StoreApi } from "zustand/vanilla";
import type { ThreadLookup } from "../../lib/thread/lookup";

/** What every entry in a thread reads, so ThreadMessages needn't drill it through the forest. */
export type ThreadView = {
  lookup: ThreadLookup;
  /** the message the reply box answers */
  answerExtId: string | undefined;
  aim: (extId: string) => void;
  flip: (personId: number, next: boolean) => void;
  pulling: string | null;
  pull: (extId: string) => void;
  landed: string | null;
  endLanding: () => void;
};

export const ThreadViewContext = createContext<StoreApi<ThreadView> | null>(null);

export function useThreadView<T>(selector: (view: ThreadView) => T): T {
  const store = useContext(ThreadViewContext);
  if (!store) throw new Error("useThreadView must be used within ThreadViewProvider");
  return useStore(store, selector);
}
