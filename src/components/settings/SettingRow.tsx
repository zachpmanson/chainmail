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
    <div className="flex items-center gap-4 py-3 max-[640px]:flex-col max-[640px]:items-stretch max-[640px]:gap-2">
      <div className="min-w-0 flex-1">
        <h3 className="m-0 text-xs font-semibold">{title}</h3>
        <p className="mt-1 text-xs/snug text-muted">{description}</p>
      </div>
      <div
        className={`flex min-w-56 flex-[.9] flex-col items-start gap-1 max-[640px]:min-w-0${valueClassName ? `${valueClassName}` : ""}`}
      >
        {children}
        {note ? <p className="m-0 wrap-break-word text-xs/snug text-muted">{note}</p> : null}
      </div>
    </div>
  );
}
