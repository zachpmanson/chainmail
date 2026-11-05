import { useIsFetching } from "@tanstack/react-query";

/** Loading text in the deploy-stamp slot; the version query stays mounted behind it. */
export default function NavReading() {
  const reading = useIsFetching();
  const reminders = useIsFetching({
    predicate: (query) =>
      typeof query.queryKey[1] === "string" && query.queryKey[1].startsWith("/v1/hassles"),
  });
  if (reading === 0) return null;
  return (
    <span className="text-xs whitespace-nowrap text-muted" role="status">
      {reminders > 0 ? "Reading reminders…" : "Reading corpus…"}
    </span>
  );
}
