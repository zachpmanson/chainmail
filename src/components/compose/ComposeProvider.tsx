import type { ReactNode } from "react";
import { ComposeContext, type ComposeContextValue } from "./ComposeContext";

export default function ComposeProvider({
  children,
  composing,
  closeCompose,
}: ComposeContextValue & { children: ReactNode }) {
  return (
    <ComposeContext.Provider value={{ composing, closeCompose }}>
      {children}
    </ComposeContext.Provider>
  );
}
