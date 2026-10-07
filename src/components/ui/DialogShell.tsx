import type { MouseEvent, ReactNode } from "react";

const panelBase =
  "proposals-panel flex max-h-[70vh] max-w-[44rem] flex-col rounded-lg border border-line bg-card shadow-[0_8px_40px_rgba(0,0,0,.35)]";

/** Shared accessible overlay and panel framing; dialog content and dismissal
 * policy stay with the owning feature. */
export function DialogShell({
  label,
  children,
  panelClassName = "",
  onBackdropClick,
}: {
  label: string;
  children: ReactNode;
  panelClassName?: string;
  onBackdropClick?: () => void;
}) {
  const backdropClick = (event: MouseEvent<HTMLDivElement>) => {
    if (event.target === event.currentTarget) onBackdropClick?.();
  };
  return (
    <div
      className="proposals"
      role="dialog"
      aria-modal="true"
      aria-label={label}
      onClick={backdropClick}
    >
      <div className={`${panelBase} ${panelClassName}`.trim()}>{children}</div>
    </div>
  );
}
