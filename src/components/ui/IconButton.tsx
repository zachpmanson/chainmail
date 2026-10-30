import { forwardRef, type AnchorHTMLAttributes, type ButtonHTMLAttributes, type Ref } from "react";
import { cn } from "../../lib/ui/cn";
import IconFrame from "./IconFrame";
import IconTooltip from "./IconTooltip";

/** A glyph-only control: a link when given `href`, otherwise a button. */
export default forwardRef<
  HTMLAnchorElement | HTMLButtonElement,
  | ({ href: string } & AnchorHTMLAttributes<HTMLAnchorElement>)
  | ({ href?: undefined } & ButtonHTMLAttributes<HTMLButtonElement>)
>(function IconButton({ className = "", children, ...props }, ref) {
  const classes = cn(
    "group/icon inline-flex shrink-0 cursor-pointer rounded-md border-0 bg-transparent p-0 no-underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed",
    className,
  );
  if (props.href !== undefined)
    return (
      <IconTooltip>
        <a ref={ref as Ref<HTMLAnchorElement>} className={classes} {...props}>
          <IconFrame>{children}</IconFrame>
        </a>
      </IconTooltip>
    );
  return (
    <IconTooltip>
      <button ref={ref as Ref<HTMLButtonElement>} type="button" className={classes} {...props}>
        <IconFrame>{children}</IconFrame>
      </button>
    </IconTooltip>
  );
});
