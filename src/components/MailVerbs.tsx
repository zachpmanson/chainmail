import type { QueryClient } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";
import { ArchiveBoxIcon, FolderIcon, TrashIcon } from "@heroicons/react/24/outline";
import { $api, ApiError } from "../lib/api";

/**
 * The two mailbox verbs — their glyphs, what they leave to say, and what they make
 * stale — in one place, because two surfaces now reach for the same pair.
 *
 * The selection bar has them for the threads a reader has ticked (see ActionBar);
 * the reading pane has them for the thread that is open (see ThreadPane). They are
 * the same verbs in the same words either way: /v1/mail takes a set of chains and
 * an action, and a set of one is a set. The sentence a reader is shown after
 * archiving is an account of the same write whichever page made it, so it is
 * written once rather than twice — which is the drift this file exists to stop
 * (the bar and the search page's copy of it had already parted company once).
 */

/**
 * How long the account of what just happened stays up.
 *
 * Long enough to read a count and the line about entries with no mailbox copy,
 * and short enough that it is gone by the time the reader has finished reading the
 * list it changed. In the bar the sentence stands in the header's own row, so a
 * sentence left there covers the top of the page for the rest of the session; in
 * the pane it sits under a thread that has just left the list. Both are accounts
 * of work that is over rather than states of the mailbox the page should keep
 * reporting, so both are on the same clock.
 *
 * A refusal is not on this timer: it is something to act on, and an error that
 * took itself away would leave the reader looking at mail that did not move with
 * no explanation of why.
 */
export const SAID_MS = 5000;

/**
 * The box: what archive means in a mail client, and what is left after it. A box
 * with a lid and a slot rather than a folder, because a folder is where a move
 * goes and this is the verb that takes mail out of the way without naming a
 * place.
 */
export function ArchiveGlyph() {
  return <ArchiveBoxIcon width={14} height={14} aria-hidden="true" />;
}

/** The bin. Delete is a move to the trash in the mailbox's own words, and this
 *  is the glyph that says so without the word. */
export function TrashGlyph() {
  return <TrashIcon width={14} height={14} aria-hidden="true" />;
}

/**
 * What to tell the reader after a mail action, built from what the service
 * answered rather than from what was asked for: the counts are the mailbox's.
 *
 * The skipped count is named only when there is one, because it is the exception
 * — an entry that survived only inside somebody's quote, or a Slack post, has no
 * mailbox copy to move. It is not a failure and it is not silent either: a reader
 * who moved four threads and is told about three messages needs to know why the
 * number is not what they can see.
 */
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

/**
 * What a mail action makes stale, whichever surface ran it.
 *
 * The mail moved, so everything that counts or lists mail is out of date: the
 * list the reader is looking at, the folder counts, the corpus totals, and the
 * open thread's own labels. The list is the answer they are reading, so it is
 * re-asked rather than patched — a row that quietly disappeared would be the page
 * claiming a write it had not been told the result of.
 */
export function staleAfterMail(qc: QueryClient): void {
  void qc.invalidateQueries({ queryKey: ["get", "/v1/search"] });
  void qc.invalidateQueries({ queryKey: ["get", "/v1/labels"] });
  void qc.invalidateQueries({ queryKey: ["get", "/v1/stats"] });
  void qc.invalidateQueries({ queryKey: ["get", "/v1/chains/{rootExtId}"] });
}

/**
 * The move control: one dropdown, worn as the icon button the two verbs beside it
 * are worn as.
 *
 * It used to be the word "Move…" on a select shaped like a button. The word earns
 * nothing on a row that already reads by glyphs, and the reader knows the folder
 * before they reach for it — what they need is the control, not its name. So the
 * face is a folder in the same box as the box and the bin (see .ibicon), and the
 * dropdown is the platform's own <select>, laid over that box invisibly: it is what
 * still knows where a reader's folders are, what opens where the click happened, and
 * what a keyboard and a screen reader expect from a choice of folders. Clicking the
 * glyph is clicking the select.
 *
 * When the current folder is known it is selected directly, so a placeholder would
 * only be a spurious extra choice. Without that context, the placeholder keeps the
 * control legible before a destination is chosen. The name is on both the box (the
 * tooltip) and the select (what a screen reader reads), so the icon is never a
 * picture standing where
 * a control should be.
 */
type MoveAccount = {
  id: string;
  email?: string;
  displayName: string;
};

function AccountFolderOptions({ account }: { account: MoveAccount }) {
  const labels = $api.useQuery("get", "/v1/labels", {
    params: { query: { accountId: account.id } },
  });
  const folders = (labels.data?.labels ?? [])
    .map((label) => label.name)
    .filter((name) => name !== "INBOX")
    .sort((a, b) => a.localeCompare(b));

  return (
    <optgroup label={account.email || account.displayName || account.id}>
      {labels.isPending ? <option value="" disabled>Loading folders…</option> : null}
      {labels.isError ? <option value="" disabled>Folders unavailable</option> : null}
      {!labels.isPending && !labels.isError && folders.length === 0 ? (
        <option value="" disabled>No folders available</option>
      ) : null}
      {folders.map((folder) => (
        <option key={folder} value={JSON.stringify([account.id, folder])}>
          {folder}
        </option>
      ))}
    </optgroup>
  );
}

/**
 * The move control: labels and the write that uses them stay scoped to the same
 * Gmail account. In an account-filtered view that is the selected account; in an
 * all-accounts view each option carries its account explicitly, so a folder from
 * one mailbox is never silently offered as a destination in another.
 */
export function MoveFolder({
  defaultFolder,
  busy,
  onMove,
}: {
  /** Current folder, selected when the view is scoped to one account. */
  defaultFolder?: string;
  /** Whether a write is in flight. */
  busy: boolean;
  onMove: (folder: string, accountId?: string) => void;
}) {
  const routeAccountId = useSearch({ from: "/" }).accountId;
  const auth = $api.useQuery("get", "/auth/status", {});
  const accounts = (auth.data?.accounts ?? []).filter((account) => account.signedIn);
  const accountId = routeAccountId || (accounts.length === 1 ? accounts[0]?.id : undefined);
  const allAccounts = !routeAccountId && accounts.length > 1;
  const labels = $api.useQuery(
    "get",
    "/v1/labels",
    { params: { query: accountId ? { accountId } : {} } },
    { enabled: Boolean(routeAccountId) || (!auth.isPending && !auth.isError && accounts.length <= 1) },
  );
  const folders = (labels.data?.labels ?? [])
    .map((label) => label.name)
    .filter((name) => name !== "INBOX")
    .sort((a, b) => a.localeCompare(b));
  const pending = auth.isPending || (!allAccounts && labels.isPending);
  const unavailable = auth.isError || (!allAccounts && labels.isError);

  const move = (value: string) => {
    if (!value) return;
    if (!allAccounts) {
      onMove(value, accountId);
      return;
    }
    try {
      const parsed: unknown = JSON.parse(value);
      if (Array.isArray(parsed) && typeof parsed[0] === "string" && typeof parsed[1] === "string") {
        onMove(parsed[1], parsed[0]);
      }
    } catch {
      // The placeholder and disabled loading/error options never carry a target.
    }
  };

  return (
    <span className="ibicon ibmovewrap" title="Move to a folder">
      <FolderIcon width={14} height={14} aria-hidden="true" />
      <select
        className="ibmove"
        aria-label="Move to a folder"
        value={allAccounts ? "" : defaultFolder ?? ""}
        disabled={busy || auth.isPending || auth.isError || (!allAccounts && (labels.isPending || labels.isError))}
        onChange={(event) => move(event.target.value)}
      >
        {!allAccounts && defaultFolder && !folders.includes(defaultFolder) ? (
          <option value={defaultFolder} disabled>
            {defaultFolder === "INBOX" ? "Inbox" : defaultFolder} (current folder)
          </option>
        ) : null}
        {!allAccounts ? (
          <>
            {!defaultFolder ? <option value="">{pending ? "Loading folders…" : unavailable ? "Folders unavailable" : "Move…"}</option> : null}
            {folders.map((folder) => (
              <option key={folder} value={folder}>{folder}</option>
            ))}
          </>
        ) : (
          <>
            <option value="">Move…</option>
            {accounts.map((account) => (
              <AccountFolderOptions key={account.id} account={account} />
            ))}
          </>
        )}
      </select>
    </span>
  );
}

/** The verb as a reader would say it, for the line a failure is reported on: a
 *  reader who pressed the bin is not told that "mail action" failed. */
export const VERBS: Record<string, string> = {
  archive: "Archiving",
  trash: "Deleting",
  move: "Moving",
};

/**
 * Why a mailbox write was refused, in words a reader can act on.
 *
 * A 403 is the host's own answer rather than a failure of the write: it was
 * started without the switch that allows this one, and each write has its own
 * switch (-mark-read and -mail-write are separate on purpose — see cmd/server, a
 * host that lets the read circle write has not thereby asked for delete). Naming
 * the switch is the difference between a reader who can fix it and one looking at
 * a button that does nothing.
 */
export function refusal(e: unknown, flag: string, what: string): string {
  return e instanceof ApiError && e.status === 403
    ? `This host cannot change the mailbox: it was started without ${flag}.`
    : `${what} failed: ${e instanceof Error ? e.message : String(e)}`;
}
