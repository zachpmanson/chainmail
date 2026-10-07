import { ApiError } from "../lib/api";

/** Names a failed request so distinct server responses give distinct instructions. */
export function statusLabel(status: number): string {
  if (status === 404) return "Not found";
  if (status === 400) return "Rejected";
  if (status === 429) return "Busy";
  if (status === 503) return "Service unavailable";
  if (status === 504) return "Timed out";
  return `Error ${status}`;
}

export function Failure({ error }: { error: unknown }) {
  const api = error instanceof ApiError ? error : null;
  return (
    <p
      className="selfail mt-[.7rem] rounded-md border border-line border-l-[3px] border-l-red-700 bg-card px-[.7rem] py-2 text-[.82rem]"
      role="alert"
    >
      <strong>
        {api ? `${statusLabel(api.status)} (${api.status})` : "Could not reach the service"}
      </strong>{" "}
      <span>{error instanceof Error ? error.message : String(error)}</span>
    </p>
  );
}

/** Thread metadata shared by list rows, previews, and the reading pane. */
export interface PreviewableThread {
  rootExtId: string;
  subject?: string;
  entries?: number;
  people?: number;
  attachments?: number;
  unread?: number;
}
