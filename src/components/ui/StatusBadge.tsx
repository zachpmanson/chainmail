import type { ReactNode } from "react";

const tones = {
  new: "rounded-[4px] bg-org-1 px-1 py-px text-[.6rem] font-extrabold uppercase tracking-[.09em] text-white",
  revised:
    "rounded-[4px] border border-muted bg-dash px-1 text-[.6rem] font-extrabold uppercase tracking-[.09em] text-fg",
  success:
    "whitespace-nowrap rounded-full border border-current px-1.5 py-px text-[.62rem] font-bold text-green-800",
  neutral:
    "whitespace-nowrap rounded-full border border-current px-1.5 py-px text-[.62rem] font-bold text-muted",
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
  return <span className={`${tones[tone]} ${className}`.trim()}>{children}</span>;
}
