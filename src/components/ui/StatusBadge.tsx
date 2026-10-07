import type { ReactNode } from "react";
import { cn } from "../../lib/ui/cn";

const tones = {
  new: "rounded-sm bg-org-1 px-1 py-px text-2xs font-extrabold uppercase tracking-[.09em] text-white",
  revised:
    "rounded-sm border border-muted bg-dash px-1 text-2xs font-extrabold uppercase tracking-[.09em] text-fg",
  success:
    "whitespace-nowrap rounded-full border border-current px-1.5 py-px text-2xs font-bold text-green-800",
  neutral:
    "whitespace-nowrap rounded-full border border-current px-1.5 py-px text-2xs font-bold text-muted",
} as const;

export type StatusBadgeTone = keyof typeof tones;

/** Compact, non-interactive status label with a small set of shared visual tones. */
export default function StatusBadge({
  tone,
  className = "",
  children,
}: {
  tone: StatusBadgeTone;
  className?: string;
  children: ReactNode;
}) {
  return <span className={cn(tones[tone], className)}>{children}</span>;
}
