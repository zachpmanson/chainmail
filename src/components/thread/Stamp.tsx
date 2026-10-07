import type { StampData } from "../../lib/ui/stamp";

export default function Stamp({ id, stamp }: { id: string; stamp: StampData }) {
  const { date, time, tz, zone } = stamp;
  return (
    <a
      className="whitespace-nowrap text-xs tabular-nums text-muted"
      href={`#${id}`}
      title="Link to this message"
    >
      {date}
      {time ? ` · ${time}` : ""}
      {zone === "stated" ? <span className="text-[.9em] opacity-75">{tz}</span> : null}
      {zone === "inferred" ? (
        <span
          className="border-b border-dotted border-current text-[.9em] opacity-55 cursor-help"
          title="Inferred — this source stated no zone. The offset was worked out from the client that quoted this message; see the source notes."
        >{` ${tz}?`}</span>
      ) : null}
      {zone === "unknown" ? (
        <span
          className="text-[.9em] italic tracking-[.02em] opacity-45 cursor-help"
          title="Zone unknown — this source stated none and nothing available places it. The clock is a wall clock as quoted, so it cannot be compared with the times above and below it."
        >
          {" ?"}
        </span>
      ) : null}
    </a>
  );
}
