import { useLayoutEffect, useRef, type TextareaHTMLAttributes } from "react";
import { cn } from "../../lib/ui/cn";
import { fieldBase } from "./styles";

function fit(element: HTMLTextAreaElement) {
  // Let native rows establish the minimum again before measuring content (including padding).
  element.style.height = "auto";
  const style = getComputedStyle(element);
  const padding = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom);
  const border = parseFloat(style.borderTopWidth) + parseFloat(style.borderBottomWidth);
  const adjustment = style.boxSizing === "border-box" ? border : -padding;
  element.style.height = `${Math.ceil(element.scrollHeight + adjustment)}px`;
}

export default function TextArea({
  className = "",
  autoGrow = false,
  onChange,
  ...props
}: TextareaHTMLAttributes<HTMLTextAreaElement> & { autoGrow?: boolean }) {
  const ref = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!autoGrow || !element) return;
    const originalHeight = element.style.height;
    let width = element.clientWidth;
    let frame: number | undefined;
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(() => {
            // Height writes also notify; only a changed width can change line wrapping.
            if (element.clientWidth === width) return;
            width = element.clientWidth;
            // Write outside observer delivery to avoid resize-loop warnings.
            if (frame !== undefined) cancelAnimationFrame(frame);
            frame = requestAnimationFrame(() => fit(element));
          });
    observer?.observe(element);
    return () => {
      observer?.disconnect();
      if (frame !== undefined) cancelAnimationFrame(frame);
      element.style.height = originalHeight;
    };
  }, [autoGrow]);
  useLayoutEffect(() => {
    if (autoGrow && ref.current) fit(ref.current);
  });
  return (
    <textarea
      ref={ref}
      className={cn(fieldBase, autoGrow ? "resize-none overflow-y-hidden" : "resize-y", className)}
      {...props}
      onChange={(event) => {
        onChange?.(event);
        if (autoGrow && ref.current) fit(ref.current);
      }}
    />
  );
}
