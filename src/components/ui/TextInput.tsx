import type { InputHTMLAttributes } from "react";
import { cn } from "../../lib/ui/cn";
import { fieldBase } from "./styles";

export default function TextInput({
  className = "",
  ...props
}: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={cn(fieldBase, className)} {...props} />;
}
