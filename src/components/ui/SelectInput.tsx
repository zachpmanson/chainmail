import type { SelectHTMLAttributes } from "react";
import { cn } from "../../lib/ui/cn";
import { fieldBase } from "./styles";

export default function SelectInput({
  className = "",
  ...props
}: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={cn(fieldBase, className)} {...props} />;
}
