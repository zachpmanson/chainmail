import type { TextareaHTMLAttributes } from "react";
import { cn } from "../../lib/ui/cn";
import { fieldBase } from "./styles";

export default function TextArea({
  className = "",
  ...props
}: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={cn(fieldBase, "resize-y", className)} {...props} />;
}
