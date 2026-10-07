import type { AnchorHTMLAttributes, ButtonHTMLAttributes } from "react";
import { cn } from "../../lib/ui/cn";
import IconFrame from "./IconFrame";

/** A glyph-only control: a link when given `href`, otherwise a button. */
export default function IconButton({
  className = "",
  children,
  ...props
}:
  | ({ href: string } & AnchorHTMLAttributes<HTMLAnchorElement>)
  | ({ href?: undefined } & ButtonHTMLAttributes<HTMLButtonElement>)) {
  const classes = cn(
    "group/icon inline-flex shrink-0 cursor-pointer rounded-md border-0 bg-transparent p-0 no-underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed",
    className,
  );
  if (props.href !== undefined)
    return (
      <a className={classes} {...props}>
        <IconFrame>{children}</IconFrame>
      </a>
    );
  return (
    <button type="button" className={classes} {...props}>
      <IconFrame>{children}</IconFrame>
    </button>
  );
}
