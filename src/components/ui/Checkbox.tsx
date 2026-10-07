import { forwardRef, type InputHTMLAttributes } from "react";
import { cn } from "../../lib/ui/cn";

const accentClasses = {
  native: "",
  accent: "accent-accent",
  org: "accent-org-1",
} as const;

export type CheckboxAccent = keyof typeof accentClasses;

export type CheckboxProps = Omit<InputHTMLAttributes<HTMLInputElement>, "type"> & {
  accent?: CheckboxAccent;
};

const Checkbox = forwardRef<HTMLInputElement, CheckboxProps>(function Checkbox(
  { accent = "native", className = "", ...props },
  ref,
) {
  return (
    <input
      {...props}
      ref={ref}
      type="checkbox"
      className={cn(accentClasses[accent], className) || undefined}
    />
  );
});

export default Checkbox;
