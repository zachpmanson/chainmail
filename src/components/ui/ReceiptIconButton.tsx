import { forwardRef, type ButtonHTMLAttributes } from "react";
import { cn } from "../../lib/ui/cn";
import IconTooltip from "./IconTooltip";

export default forwardRef<HTMLButtonElement, ButtonHTMLAttributes<HTMLButtonElement>>(
  function ReceiptIconButton({ className = "", ...props }, ref) {
    return (
      <IconTooltip>
        <button
          ref={ref}
          className={cn(
            "inline-flex shrink-0 cursor-pointer items-center justify-center rounded-md border border-transparent bg-transparent p-1 font-[inherit] text-muted hover:border-line hover:bg-card hover:text-accent aria-pressed:border-accent aria-pressed:bg-mine aria-pressed:text-accent [&_svg]:block [&_svg]:size-[18px]",
            className,
          )}
          {...props}
        />
      </IconTooltip>
    );
  },
);
