import { ApiError } from "../../lib/api/api";

/** How long a success toast stays up. Refusals stay until dismissed. */
export const SAID_MS = 5000;

/** Built from the server's counts. Skipped entries (quote-only, Slack) have no
 *  mailbox copy to move, so they're named to explain a short count. */
export function sentence(
  action: string,
  labels: string[] | undefined,
  changed: number,
  skipped: number,
): string {
  const messages = `${changed} message${changed === 1 ? "" : "s"}`;
  const what =
    action === "move"
      ? `Moved ${messages} to ${labels?.join(", ") ?? ""}`
      : action === "trash"
        ? `Deleted ${messages} — in the trash, recoverable for 30 days`
        : `Archived ${messages} — out of the inbox, still in All Mail`;
  return skipped > 0
    ? `${what}. ${
        skipped === 1
          ? "1 entry has no mailbox copy and was left alone"
          : `${skipped} entries have no mailbox copy and were left alone`
      }.`
    : `${what}.`;
}

export const VERBS: Record<string, string> = {
  archive: "Archiving",
  trash: "Deleting",
  move: "Moving",
};

/** A 403 means the host lacks this write's flag (see cmd/server); name the flag. */
export function refusal(e: unknown, flag: string, what: string): string {
  return e instanceof ApiError && e.status === 403
    ? `This host cannot change the mailbox: it was started without ${flag}.`
    : `${what} failed: ${e instanceof Error ? e.message : String(e)}`;
}
