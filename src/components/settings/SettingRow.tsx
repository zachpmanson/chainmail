import type { ReactNode } from "react";

/** A setting's explanation and value control, aligned as a responsive pair. */
export default function SettingRow({
  title,
  description,
  note,
  valueClassName,
  children,
}: {
  title: string;
  description: string;
  note?: ReactNode;
  valueClassName?: string;
  children: ReactNode;
}) {
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_minmax(14rem,.9fr)] items-center gap-4 py-3 max-[640px]:grid-cols-1 max-[640px]:gap-2">
      <div className="min-w-0">
        <h3 className="m-0 text-[.78rem] font-semibold">{title}</h3>
        <p className="mt-1 text-[.72rem] leading-[1.4] text-muted">{description}</p>
      </div>
      <div
        className={`flex min-w-0 flex-col items-start gap-1${valueClassName ? ` ${valueClassName}` : ""}`}
      >
        {children}
        {note ? (
          <p className="m-0 break-words text-[.7rem] leading-[1.4] text-muted">{note}</p>
        ) : null}
      </div>
    </div>
  );
}
