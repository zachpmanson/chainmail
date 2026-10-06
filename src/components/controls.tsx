import type {
  AnchorHTMLAttributes,
  ButtonHTMLAttributes,
  InputHTMLAttributes,
  SelectHTMLAttributes,
  TextareaHTMLAttributes,
} from "react";

const buttonBase =
  "inline-flex min-h-8 items-center justify-center gap-2 rounded-md border px-3 py-1.5 text-sm font-semibold transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--accent)] disabled:cursor-not-allowed disabled:opacity-50";

const buttonVariants = {
  primary:
    "border-[var(--accent)] bg-[var(--accent)] text-[var(--bg)] hover:brightness-110",
  secondary:
    "border-[var(--line)] bg-[var(--card)] text-[var(--fg)] hover:border-[var(--accent)] hover:text-[var(--accent)]",
  quiet:
    "border-transparent bg-transparent text-[var(--muted)] hover:border-[var(--line)] hover:bg-[var(--card)] hover:text-[var(--accent)]",
  danger:
    "border-red-700 bg-[var(--card)] text-red-700 hover:bg-[var(--card)]",
  subtle:
    "border-[var(--line)] bg-[var(--mine)] text-[var(--fg)] hover:border-[var(--accent)] hover:text-[var(--accent)]",
  menu: "border-0 bg-transparent text-[var(--fg)] hover:bg-[var(--mine)]",
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
  const sizing = density === "compact" ? "min-h-0 px-3 py-1 text-xs" : "";
  return (
    <button
      className={`${buttonBase} ${buttonVariants[variant]} ${sizing} ${className}`.trim()}
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
  const sizing = density === "compact" ? "min-h-0 px-3 py-1 text-xs" : "";
  return (
    <a
      className={`${buttonBase} ${buttonVariants[variant]} ${sizing} ${className}`.trim()}
      {...props}
    />
  );
}

export function IconButton({
  className = "",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <Button
      variant="quiet"
      className={`size-8 shrink-0 p-1 ${className}`.trim()}
      {...props}
    />
  );
}

const fieldBase =
  "min-h-8 min-w-0 rounded-md border border-[var(--line)] bg-[var(--card)] px-2 py-1 text-sm text-[var(--fg)] placeholder:text-[var(--muted)] focus:border-[var(--accent)] focus:outline-none focus:ring-1 focus:ring-[var(--accent)] disabled:cursor-not-allowed disabled:opacity-60";

export function TextInput({
  className = "",
  ...props
}: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={`${fieldBase} ${className}`.trim()} {...props} />;
}

export function SelectInput({
  className = "",
  ...props
}: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={`${fieldBase} ${className}`.trim()} {...props} />;
}

export function TextArea({
  className = "",
  ...props
}: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={`${fieldBase} resize-y ${className}`.trim()}
      {...props}
    />
  );
}
