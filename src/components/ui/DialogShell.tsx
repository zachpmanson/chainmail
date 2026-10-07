import type { MouseEvent, ReactNode } from "react";

export default function DialogShell({
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
      className="fixed inset-0 z-[55] flex items-center justify-center bg-[rgba(0,0,0,.45)]"
      role="dialog"
      aria-modal="true"
      aria-label={label}
      onClick={backdropClick}
    >
      <div
        className={`flex max-h-[70vh] max-w-[44rem] flex-col rounded-lg border border-line bg-card shadow-[0_8px_40px_rgba(0,0,0,.35)] ${panelClassName}`.trim()}
      >
        {children}
      </div>
    </div>
  );
}
