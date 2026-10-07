import type { ReactNode, SelectHTMLAttributes } from "react";
import IconFrame from "./IconFrame";

/** A real <select> laid invisibly over the glyph, for keyboard and screen readers.
 *  A <select> can't live inside a <button>, hence the span. */
export default function IconSelect({
  icon,
  title,
  className = "",
  children,
  ...props
}: {
  icon: ReactNode;
  title: string;
  className?: string;
  children: ReactNode;
} & Omit<SelectHTMLAttributes<HTMLSelectElement>, "className" | "title">) {
  return (
    <span
      className={`group/icon relative inline-flex shrink-0 rounded-md has-[select:focus-visible]:outline-2 has-[select:focus-visible]:outline-offset-2 has-[select:focus-visible]:outline-accent ${className}`.trim()}
      title={title}
    >
      <IconFrame>{icon}</IconFrame>
      <select
        className="absolute inset-0 m-0 h-full w-full cursor-pointer appearance-none border-0 p-0 font-[inherit] opacity-0 focus-visible:outline-none disabled:cursor-not-allowed"
        {...props}
      >
        {children}
      </select>
    </span>
  );
}
