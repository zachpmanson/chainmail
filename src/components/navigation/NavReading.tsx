import { useIsFetching } from "@tanstack/react-query";

/** Sits before the right-hand group (pinned by `margin-left:auto`) so it doesn't shift it. */
export default function NavReading() {
  const reading = useIsFetching();
  if (reading === 0) return null;
  return (
    <span className="ml-4 text-xs whitespace-nowrap text-muted" role="status">
      Reading the corpus…
    </span>
  );
}
