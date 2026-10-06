import type { ReactNode } from "react";

const tones = {
  new: "rounded-[4px] bg-org-1 px-[.3rem] py-[.05rem] text-[.6rem] font-extrabold uppercase tracking-[.09em] text-white",
  revised: "rounded-[4px] border border-muted bg-dash px-[.28rem] py-[.02rem] text-[.6rem] font-extrabold uppercase tracking-[.09em] text-fg",
  success: "whitespace-nowrap rounded-full border border-current px-[.4rem] py-[.06rem] text-[.62rem] font-bold text-green-800",
  neutral: "whitespace-nowrap rounded-full border border-current px-[.4rem] py-[.06rem] text-[.62rem] font-bold text-muted",
} as const;

export type StatusBadgeTone = keyof typeof tones;

/** Compact, non-interactive status label with a small set of shared visual tones. */
export function StatusBadge({
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
