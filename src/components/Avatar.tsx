import { initials } from "../lib/anchors";

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
      className={`av ${orgSlot}${pic ? ` pic ${pic}` : ""} grid size-[1em] flex-[0_0_1em] place-items-center self-center overflow-hidden rounded-full ${textSize} leading-none text-white`}
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
