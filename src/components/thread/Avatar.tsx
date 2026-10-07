import { initials } from "../../lib/anchors";

const orgColor = {
  o1: "bg-org-1 [--ring:var(--o1)]",
  o2: "bg-org-2 [--ring:var(--o2)]",
  o3: "bg-org-3 [--ring:var(--o3)]",
  o4: "bg-org-4 [--ring:var(--o4)]",
  o5: "bg-org-5 [--ring:var(--o5)]",
} as const;

/** A sender/participant avatar. The organization class and image class are
 * resolved by the caller so page and inbox views keep their own data pipeline. */
export function Avatar({
  name,
  orgSlot,
  pic,
  title,
  size = "message",
}: {
  name: string;
  orgSlot: string;
  pic?: string;
  title?: string;
  size?: "message" | "participant";
}) {
  const textSize = size === "participant" ? "text-[.78rem]" : "text-[.83rem]";
  return (
    <div
      className={`av ${orgSlot} ${orgColor[orgSlot as keyof typeof orgColor] ?? orgColor.o5}${pic ? ` pic ${pic} bg-cover bg-center shadow-[0_0_0_1.5px_var(--ring,var(--muted)),0_0_0_2.5px_var(--bg)]` : ""} grid size-[1em] flex-[0_0_1em] place-items-center self-center overflow-hidden rounded-full ${textSize} leading-none text-white`}
      title={title}
    >
      {pic ? null : (
        <span className="ini text-[.62em] font-bold leading-none tracking-[.01em]">
          {initials(name)}
        </span>
      )}
    </div>
  );
}
