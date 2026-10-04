import { createContext, useContext, type ReactNode } from "react";

type ComposeContextValue = { composing: boolean; closeCompose: () => void };
const ComposeContext = createContext<ComposeContextValue | null>(null);

export function ComposeProvider({ children, composing, closeCompose }: ComposeContextValue & { children: ReactNode }) {
  return <ComposeContext.Provider value={{ composing, closeCompose }}>{children}</ComposeContext.Provider>;
}

export function useCompose() {
  const value = useContext(ComposeContext);
  if (!value) throw new Error("useCompose must be used within ComposeProvider");
  return value;
}
