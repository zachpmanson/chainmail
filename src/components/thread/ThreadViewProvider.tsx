import { useEffect, useLayoutEffect, useState, type ReactNode } from "react";
import { createStore } from "zustand/vanilla";
import { ThreadViewContext, type ThreadView } from "./ThreadViewContext";

// Static renders run no effects and would warn about useLayoutEffect; they only need the first values.
const useSyncEffect = typeof document === "undefined" ? useEffect : useLayoutEffect;

/** Key it by thread: a fresh store means a switch never draws the last thread's values. */
export default function ThreadViewProvider({
  children,
  ...view
}: ThreadView & { children: ReactNode }) {
  const [store] = useState(() => createStore<ThreadView>()(() => view));
  // Before paint, so a refetch's one render on the old values is never seen.
  useSyncEffect(() => store.setState(view, true));
  return <ThreadViewContext.Provider value={store}>{children}</ThreadViewContext.Provider>;
}
