import { useEffect, useSyncExternalStore } from "react";
import { useIsMutating, useQueryClient } from "@tanstack/react-query";
import { hassleMutation, nativeOperations, ready } from "./native";

export default function useNativeOperations() {
  const store = nativeOperations(useQueryClient());
  const state = useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot);
  const mutations = useIsMutating({ predicate: hassleMutation });
  useEffect(() => {
    if (state.recovery?.status !== "busy") return;
    const timer = setTimeout(() => void store.status(), 1000);
    return () => clearTimeout(timer);
  }, [state.recovery, store]);
  return { store, state, busy: mutations > 0 || !ready(state.recovery), mutations };
}
