import type { ReactNode } from "react";

/** The settings page's shared section frame. Kept settings-specific until another screen needs it. */
export default function SettingsSection({
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
      className="min-w-0 rounded-[10px] border border-line bg-card px-4 py-4 max-[640px]:p-3"
      aria-labelledby={id}
    >
      <header className="mb-3">
        <h2 className="m-0 text-[.91rem] font-semibold tracking-[-.01em]" id={id}>
          {title}
        </h2>
        <p className="mt-1 text-[.74rem] leading-[1.45] text-muted">{description}</p>
      </header>
      {children}
    </section>
  );
}
