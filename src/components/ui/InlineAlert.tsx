import type { ReactNode } from "react";

export default function InlineAlert({
  className = "mt-3",
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  return (
    <p
      className={`rounded-md border border-line border-l-[3px] border-l-red-700 bg-card px-3 py-2 text-[.82rem] ${className}`}
      role="alert"
    >
      {children}
    </p>
  );
}
