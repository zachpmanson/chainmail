import type { ButtonHTMLAttributes } from "react";
import { buttonClasses, type ButtonDensity, type ButtonVariant } from "./styles";

/** `variant="bare"` drops the button look (and `density`) for callers that style it whole. */
export default function Button({
  variant = "secondary",
  density = "normal",
  className = "",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; density?: ButtonDensity }) {
  return <button className={buttonClasses(variant, density, className)} {...props} />;
}
