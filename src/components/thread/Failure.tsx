import { ApiError } from "../../lib/api/api";
import { errText } from "../../lib/ui/errText";
import InlineAlert from "../ui/InlineAlert";

/** Names a failed request so distinct server responses give distinct instructions. */
export function statusLabel(status: number): string {
  if (status === 404) return "Not found";
  if (status === 400) return "Rejected";
  if (status === 429) return "Busy";
  if (status === 503) return "Service unavailable";
  if (status === 504) return "Timed out";
  return `Error ${status}`;
}

export default function Failure({ error, className }: { error: unknown; className?: string }) {
  const api = error instanceof ApiError ? error : null;
  return (
    <InlineAlert className={className}>
      <strong>
        {api ? `${statusLabel(api.status)} (${api.status})` : "Could not reach the service"}
      </strong>{" "}
      <span>{errText(error)}</span>
    </InlineAlert>
  );
}
