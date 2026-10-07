import { initials } from "../../lib/timeline/anchors";

const orgColor = {
  o1: "bg-org-1 [--ring:var(--o1)]",
  o2: "bg-org-2 [--ring:var(--o2)]",
  o3: "bg-org-3 [--ring:var(--o3)]",
  o4: "bg-org-4 [--ring:var(--o4)]",
  o5: "bg-org-5 [--ring:var(--o5)]",
} as const;

export default function Avatar({
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
  return (
    <div
      className={`av ${orgSlot} ${orgColor[orgSlot as keyof typeof orgColor] ?? orgColor.o5}${pic ? ` ${pic} bg-cover bg-center shadow-[0_0_0_1.5px_var(--ring,var(--muted)),0_0_0_2.5px_var(--bg)]` : ""} grid size-[1em] flex-[0_0_1em] place-items-center self-center overflow-hidden rounded-full ${size === "participant" ? "text-[.78rem]" : "text-[.83rem]"} leading-none text-white`}
      title={title}
    >
      {pic ? null : (
        <span className="text-[.62em] font-bold leading-none tracking-[.01em]">
          {initials(name)}
        </span>
      )}
    </div>
  );
}
