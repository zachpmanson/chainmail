import type { ReactNode } from "react";

/** One recipients row; the fixed label width lines the fields up across rows. */
export default function HeaderRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 items-start gap-1">
      <span className="inline-flex h-7 w-9 shrink-0 items-center">{label}</span>
      <div className="flex min-w-0 flex-1">{children}</div>
    </div>
  );
}
