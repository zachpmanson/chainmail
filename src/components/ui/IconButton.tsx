import type { AnchorHTMLAttributes, ButtonHTMLAttributes } from "react";
import IconFrame from "./IconFrame";

/** A glyph-only control: a link when given `href`, otherwise a button. */
export default function IconButton({
  className = "",
  children,
  ...props
}:
  | ({ href: string } & AnchorHTMLAttributes<HTMLAnchorElement>)
  | ({ href?: undefined } & ButtonHTMLAttributes<HTMLButtonElement>)) {
  const classes =
    `group/icon inline-flex shrink-0 cursor-pointer rounded-md border-0 bg-transparent p-0 no-underline disabled:cursor-not-allowed focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${className}`.trim();
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
