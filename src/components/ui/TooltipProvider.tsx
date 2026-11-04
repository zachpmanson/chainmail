import { Tooltip } from "@base-ui/react/tooltip";
import { createContext, type ReactNode } from "react";

// Static transcripts have no React runtime; keep their native title hints instead.
export const TooltipsEnabled = createContext(false);

export default function TooltipProvider({ children }: { children: ReactNode }) {
  return (
    <TooltipsEnabled.Provider value={true}>
      <Tooltip.Provider>{children}</Tooltip.Provider>
    </TooltipsEnabled.Provider>
  );
}
