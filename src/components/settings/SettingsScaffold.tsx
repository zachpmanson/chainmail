import type { ReactNode } from "react";

/** The settings page's shared section frame. Kept settings-specific until another screen needs it. */
export function SettingsSection({
  id,
  title,
  description,
  children,
}: {
  id: string;
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <section
      className="stsection min-w-0 rounded-[10px] border border-[var(--line)] bg-[var(--card)] px-4 py-[.95rem] max-[640px]:p-[.8rem]"
      aria-labelledby={id}
    >
      <header className="mb-[.7rem]">
        <h2 className="m-0 text-[.91rem] font-semibold tracking-[-.01em]" id={id}>
          {title}
        </h2>
        <p className="mt-1 text-[.74rem] leading-[1.45] text-[var(--muted)]">{description}</p>
      </header>
      {children}
    </section>
  );
}

/** A setting's explanation and value control, aligned as a responsive pair. */
export function SettingRow({
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
    <div className="stsetting-row grid grid-cols-[minmax(0,1fr)_minmax(14rem,.9fr)] items-center gap-4 py-[.78rem] max-[640px]:grid-cols-1 max-[640px]:gap-[.55rem]">
      <div className="stsetting-copy min-w-0">
        <h3 className="m-0 text-[.78rem] font-semibold">{title}</h3>
        <p className="mt-[.2rem] text-[.72rem] leading-[1.4] text-[var(--muted)]">{description}</p>
      </div>
      <div
        className={`stsetting-value flex min-w-0 flex-col items-start gap-1${valueClassName ? ` ${valueClassName}` : ""}`}
      >
        {children}
        {note ? (
          <p className="stsetting-note m-0 break-words text-[.7rem] leading-[1.4] text-[var(--muted)]">
            {note}
          </p>
        ) : null}
      </div>
    </div>
  );
}
