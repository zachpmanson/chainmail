import type { ButtonHTMLAttributes } from "react";
import { cn } from "../../lib/ui/cn";

export default function ReceiptIconButton({
  className = "",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      className={cn(
        "inline-flex shrink-0 items-center justify-center rounded-md border border-transparent bg-transparent p-1 font-[inherit] text-muted cursor-pointer hover:border-line hover:bg-card hover:text-accent aria-pressed:border-accent aria-pressed:bg-mine aria-pressed:text-accent [&_svg]:block [&_svg]:size-[18px]",
        className,
      )}
      {...props}
    />
  );
}
