import type { ReactNode } from "react";

/** Reacts to hover/focus via the `group/icon` set by IconButton and IconSelect. */
export default function IconFrame({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex size-8 shrink-0 items-center justify-center rounded-md border border-transparent text-muted group-hover/icon:border-line group-hover/icon:bg-card group-hover/icon:text-accent group-aria-pressed/icon:border-line group-aria-pressed/icon:text-accent group-disabled/icon:opacity-50 group-has-[select:disabled]/icon:opacity-50 [&_svg]:block [&_svg]:size-6 [&_svg]:shrink-0">
      {children}
    </span>
  );
}
