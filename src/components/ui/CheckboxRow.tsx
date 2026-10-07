import type { ReactNode } from "react";
import Checkbox from "./Checkbox";
import { type CheckboxProps } from "./Checkbox";

export type CheckboxRowProps = Omit<CheckboxProps, "className"> & {
  children: ReactNode;
  className?: string;
  inputClassName?: string;
  title?: string;
};

/** A label-sized hit target that keeps its checkbox and text associated. */
export default function CheckboxRow({
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
