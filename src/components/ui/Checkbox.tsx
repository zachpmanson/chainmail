import { forwardRef, type InputHTMLAttributes, type ReactNode } from "react";

const accentClasses = {
  native: "",
  accent: "accent-accent",
  org: "accent-org-1",
} as const;

export type CheckboxAccent = keyof typeof accentClasses;

export type CheckboxProps = Omit<InputHTMLAttributes<HTMLInputElement>, "type"> & {
  accent?: CheckboxAccent;
};

/** A checkbox input with an explicit accent choice; native browser styling is
 * available for places where the existing UI deliberately uses it. */
export const Checkbox = forwardRef<HTMLInputElement, CheckboxProps>(function Checkbox(
  { accent = "native", className = "", ...props },
  ref,
) {
  return (
    <input
      {...props}
      ref={ref}
      type="checkbox"
      className={[accentClasses[accent], className].filter(Boolean).join(" ") || undefined}
    />
  );
});

export type CheckboxRowProps = Omit<CheckboxProps, "className"> & {
  children: ReactNode;
  className?: string;
  inputClassName?: string;
  title?: string;
};

/** A label-sized hit target that keeps its checkbox and text associated. */
export function CheckboxRow({
  children,
  className = "",
  inputClassName = "",
  title,
  ...inputProps
}: CheckboxRowProps) {
  return (
    <label className={className} title={title}>
      <Checkbox {...inputProps} className={inputClassName} />
      {children}
    </label>
  );
}
