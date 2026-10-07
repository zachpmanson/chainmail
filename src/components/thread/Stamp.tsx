import type { StampData } from "../../lib/ui/stamp";

export default function Stamp({ id, stamp }: { id: string; stamp: StampData }) {
  const { date, time, tz, zone } = stamp;
  return (
    <a
      className="text-xs whitespace-nowrap text-muted tabular-nums"
      href={`#${id}`}
      title="Link to this message"
    >
      {date}
      {time ? ` · ${time}` : ""}
      {zone === "stated" ? <span className="text-[.9em] opacity-75">{tz}</span> : null}
      {zone === "inferred" ? (
        <span
          className="cursor-help border-b border-dotted border-current text-[.9em] opacity-55"
          title="Inferred — this source stated no zone. The offset was worked out from the client that quoted this message; see the source notes."
        >{` ${tz}?`}</span>
      ) : null}
      {zone === "unknown" ? (
        <span
          className="cursor-help text-[.9em] tracking-[.02em] italic opacity-45"
          title="Zone unknown — this source stated none and nothing available places it. The clock is a wall clock as quoted, so it cannot be compared with the times above and below it."
        >
          {" ?"}
        </span>
      ) : null}
    </a>
  );
}
