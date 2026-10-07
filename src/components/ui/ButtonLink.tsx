import type { AnchorHTMLAttributes } from "react";
import { buttonClasses, type ButtonDensity, type ButtonVariant } from "./styles";

export default function ButtonLink({
  variant = "secondary",
  density = "normal",
  className = "",
  ...props
}: AnchorHTMLAttributes<HTMLAnchorElement> & { variant?: ButtonVariant; density?: ButtonDensity }) {
  return <a className={buttonClasses(variant, density, className)} {...props} />;
}
