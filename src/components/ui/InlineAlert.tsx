import type { ReactNode } from "react";
import { cn } from "../../lib/ui/cn";

export default function InlineAlert({
  compact = false,
  className = "mt-3",
  children,
}: {
  /** The flatter, smaller box the settings panels use. */
  compact?: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <p
      className={cn(
        compact
          ? "mb-0 rounded-sm border-l-3 border-l-red-700 bg-bg px-3 py-2 text-xs/normal wrap-break-word text-fg"
          : "rounded-md border border-l-3 border-line border-l-red-700 bg-card px-3 py-2 text-sm",
        className,
      )}
      role="alert"
    >
      {children}
    </p>
  );
}
