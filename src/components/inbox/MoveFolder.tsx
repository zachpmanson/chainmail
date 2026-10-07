import { useSearch } from "@tanstack/react-router";
import { FolderIcon } from "@heroicons/react/24/outline";
import { $api } from "../../lib/api/api";
import { pushToast } from "../../lib/ui/toasts";
import IconSelect from "../ui/IconSelect";

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
      {labels.isPending ? (
        <option value="" disabled>
          Loading folders…
        </option>
      ) : null}
      {labels.isError ? (
        <option value="" disabled>
          Folders unavailable
        </option>
      ) : null}
      {!labels.isPending && !labels.isError && folders.length === 0 ? (
        <option value="" disabled>
          No folders available
        </option>
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
 * A native <select> laid invisibly over the icon, so keyboard and screen readers get
 * a real select. Options carry their account so a move never crosses mailboxes.
 */
export default function MoveFolder({
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
    {
      enabled:
        Boolean(routeAccountId) || (!auth.isPending && !auth.isError && accounts.length <= 1),
    },
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
    // Every real option's value is JSON [accountId, folder]; anything else is a bug.
    let parsed: unknown;
    try {
      parsed = JSON.parse(value);
    } catch {
      parsed = undefined;
    }
    if (!Array.isArray(parsed) || typeof parsed[0] !== "string" || typeof parsed[1] !== "string") {
      console.error("MoveFolder: option value is not [accountId, folder]:", value);
      pushToast("Moving failed: that folder option names no account and folder.", "fail");
      return;
    }
    onMove(parsed[1], parsed[0]);
  };

  return (
    <IconSelect
      icon={<FolderIcon aria-hidden="true" />}
      title="Move to a folder"
      aria-label="Move to a folder"
      value={allAccounts ? "" : (defaultFolder ?? "")}
      disabled={
        busy ||
        auth.isPending ||
        auth.isError ||
        (!allAccounts && (labels.isPending || labels.isError))
      }
      onChange={(event) => move(event.target.value)}
    >
      {!allAccounts && defaultFolder && !folders.includes(defaultFolder) ? (
        <option value={defaultFolder} disabled>
          {defaultFolder === "INBOX" ? "Inbox" : defaultFolder} (current folder)
        </option>
      ) : null}
      {!allAccounts ? (
        <>
          {!defaultFolder ? (
            <option value="">
              {pending ? "Loading folders…" : unavailable ? "Folders unavailable" : "Move…"}
            </option>
          ) : null}
          {folders.map((folder) => (
            <option key={folder} value={folder}>
              {folder}
            </option>
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
    </IconSelect>
  );
}
