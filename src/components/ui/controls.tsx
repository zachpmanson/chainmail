import type { AnchorHTMLAttributes, ButtonHTMLAttributes } from "react";

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

export type ButtonVariant = keyof typeof buttonVariants;

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  density?: "normal" | "compact";
}

/** Shared button foundation; `className` remains available for layout and legacy hooks. */
export function Button({
  variant = "secondary",
  density = "normal",
  className = "",
  ...props
}: ButtonProps) {
  return (
    <button
      className={`${buttonBase} ${buttonVariants[variant]} ${density === "compact" ? "min-h-0 px-3 py-1 text-xs" : ""} ${className}`.trim()}
      {...props}
    />
  );
}

export interface ControlLinkProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  variant?: ButtonVariant;
  density?: "normal" | "compact";
}

export function ControlLink({
  variant = "secondary",
  density = "normal",
  className = "",
  ...props
}: ControlLinkProps) {
  return (
    <a
      className={`${buttonBase} ${buttonVariants[variant]} ${density === "compact" ? "min-h-0 px-3 py-1 text-xs" : ""} ${className}`.trim()}
      {...props}
    />
  );
}
