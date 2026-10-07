import { createContext, useContext } from "react";

export type ComposeContextValue = { composing: boolean; closeCompose: () => void };
export const ComposeContext = createContext<ComposeContextValue | null>(null);

export function useCompose() {
  const value = useContext(ComposeContext);
  if (!value) throw new Error("useCompose must be used within ComposeProvider");
  return value;
}
