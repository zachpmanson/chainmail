import { cn } from "../../lib/ui/cn";

const buttonBase =
  "inline-flex min-h-8 items-center justify-center gap-2 rounded-md border px-3 py-1.5 font-[inherit] text-sm font-semibold transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-50";

const buttonVariants = {
  primary: "border-accent bg-accent text-bg hover:brightness-110",
  secondary: "border-line bg-card text-fg hover:border-accent hover:text-accent",
  quiet:
    "border-transparent bg-transparent text-muted hover:border-line hover:bg-card hover:text-accent",
  danger: "border-red-700 bg-card text-red-700 hover:bg-card",
  subtle: "border-line bg-mine text-fg hover:border-accent hover:text-accent",
  menu: "border-0 bg-transparent text-fg hover:bg-mine",
} as const;

// Undoes the UA button styles (no preflight) and nothing more; the caller styles the rest.
const bareButton =
  "cursor-pointer border-0 bg-transparent p-0 font-[inherit] text-[length:inherit] leading-[inherit] font-[weight:inherit] text-inherit focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed";

export type ButtonVariant = keyof typeof buttonVariants | "bare";

export type ButtonDensity = "normal" | "compact";

export function buttonClasses(variant: ButtonVariant, density: ButtonDensity, className: string) {
  if (variant === "bare") return cn(bareButton, className);
  return cn(
    buttonBase,
    buttonVariants[variant],
    density === "compact" && "min-h-0 py-1 text-xs",
    className,
  );
}

export const fieldBase =
  "min-h-8 min-w-0 rounded-md border border-line bg-card px-2 py-1 font-[inherit] text-sm text-fg placeholder:text-muted focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent disabled:cursor-not-allowed disabled:opacity-60";
