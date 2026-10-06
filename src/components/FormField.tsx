import type { ReactNode } from "react";

/** Label/control association without prescribing a field's layout or sizing. */
export function FormField({
  label,
  children,
  className = "",
  labelClassName = "",
  htmlFor,
}: {
  label: ReactNode;
  children: ReactNode;
  className?: string;
  labelClassName?: string;
  htmlFor?: string;
}) {
  return (
    <label className={className} htmlFor={htmlFor}>
      <span className={labelClassName || undefined}>{label}</span>
      {children}
    </label>
  );
}
