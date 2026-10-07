import type { ReactNode } from "react";

const baseClassName =
  "selfail mt-[.7rem] rounded-md border border-line border-l-[3px] border-l-red-700 bg-card px-[.7rem] py-2 text-[.82rem]";

/** Shared inline alert surface; callers own the message and any contextual spacing. */
export function InlineAlert({ children }: { children: ReactNode }) {
  return (
    <p className={baseClassName} role="alert">
      {children}
    </p>
  );
}
