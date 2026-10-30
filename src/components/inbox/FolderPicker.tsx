import { Menu } from "@base-ui/react/menu";
import { CheckIcon, ChevronDownIcon } from "@heroicons/react/24/outline";
import { $api } from "../../lib/api/api";
import { cn } from "../../lib/ui/cn";
import { buttonClasses } from "../ui/styles";

const menuItemClasses = buttonClasses(
  "menu",
  "normal",
  "w-full justify-start gap-2 rounded-md px-2 py-1 text-left text-xs font-normal aria-current:font-semibold data-highlighted:bg-mine",
);

function FolderRows({
  accountId,
  currentAccountId,
  current,
  onPick,
}: {
  accountId?: string;
  currentAccountId?: string;
  current: string;
  onPick: (name: string, accountId?: string) => void;
}) {
  const folders = $api.useQuery("get", "/v1/labels", {
    params: { query: accountId ? { accountId } : {} },
  });
  const labels = folders.data?.labels ?? [];
  const selectedAccount = accountId === currentAccountId;
  const pick = (name: string) => onPick(name, accountId);

  return (
    <>
      {folders.isPending ? (
        <p className="mx-2 my-1.5 text-xs/snug text-muted">Reading folders…</p>
      ) : null}
      {folders.isError ? (
        <p className="mx-2 my-1.5 text-xs/snug text-muted" role="alert">
          The folder list could not be read.
        </p>
      ) : null}
      <Menu.Item
        className={cn(menuItemClasses, accountId && "pl-4")}
        aria-current={selectedAccount && !current ? "true" : undefined}
        onClick={() => pick("")}
      >
        All mail
      </Menu.Item>
      {labels.map((label) => {
        const selected = selectedAccount && label.name === current;
        return (
          <Menu.Item
            key={label.name}
            label={label.name}
            aria-current={selected ? "true" : undefined}
            className={cn(menuItemClasses, accountId && "pl-4")}
            onClick={() => pick(label.name)}
          >
            <span className="min-w-0 flex-1 truncate">{label.name}</span>
            <span className="text-xs text-muted tabular-nums">{label.messages}</span>
          </Menu.Item>
        );
      })}
      {!folders.isPending && !folders.isError && labels.length === 0 ? (
        <p className="mx-2 my-1.5 text-xs/snug text-muted">
          No message carries a label yet — the mailbox's own labels are what this list is, so it is
          empty rather than invented.
        </p>
      ) : null}
    </>
  );
}

/** The shared account-grouped folder dropdown used by the inbox and its default-folder setting. */
export default function FolderPicker({
  current,
  currentAccountId,
  isDefault = false,
  mode = "inbox",
  ariaLabel,
  disabled = false,
  onPick,
  onDefault,
}: {
  /** The folder shown in the list or selected as the home default. */
  current: string;
  currentAccountId?: string;
  isDefault?: boolean;
  mode?: "inbox" | "default";
  ariaLabel?: string;
  disabled?: boolean;
  onPick: (name: string, accountId?: string) => void;
  onDefault?: (on: boolean) => void;
}) {
  const auth = $api.useQuery("get", "/auth/status", {});
  const accounts = (auth.data?.accounts ?? []).filter((account) => account.signedIn);
  const currentAccount = accounts.find((account) => account.id === currentAccountId);
  const accountName = currentAccount?.email || currentAccount?.displayName || currentAccountId;
  const selectingDefault = mode === "default";

  return (
    <Menu.Root modal={false} disabled={disabled}>
      <div className="relative z-30 m-0" data-folders>
        <Menu.Trigger
          className={buttonClasses(
            "secondary",
            "normal",
            "w-full justify-start gap-1.5 px-2 py-1.5 text-left text-xs font-semibold hover:border-muted",
          )}
          aria-label={ariaLabel}
          disabled={disabled}
        >
          <span className="min-w-0 flex-1 truncate">
            <span className="font-bold">{current || "All mail"}</span>
            {currentAccountId ? (
              <>
                {" "}
                <span className="font-normal text-muted">({accountName})</span>
              </>
            ) : null}
          </span>
          <ChevronDownIcon className="size-3 shrink-0 text-muted" aria-hidden="true" />
        </Menu.Trigger>
      </div>

      <Menu.Portal>
        {/* Stay below either trigger, without locking page scroll or flipping above it. */}
        <Menu.Positioner
          side="bottom"
          align="start"
          sideOffset={4}
          positionMethod="fixed"
          collisionPadding={0}
          collisionAvoidance={{ side: "none", align: "shift", fallbackAxisSide: "none" }}
          className="z-31"
        >
          <Menu.Popup
            data-folders
            className="box-border w-(--anchor-width) overflow-y-auto rounded-lg border border-line bg-card p-1 shadow-[0_8px_24px_rgba(0,0,0,.22)]"
            style={{ maxHeight: "max(0px, calc(var(--available-height) - 2rem))" }}
            aria-label="Folders"
          >
            {!selectingDefault ? (
              <Menu.CheckboxItem
                checked={isDefault}
                closeOnClick={false}
                className={menuItemClasses}
                onCheckedChange={(checked) => onDefault?.(checked)}
              >
                <span className="min-w-0 flex-1 truncate">
                  Open {current || "All mail"}
                  {currentAccountId && accountName ? ` (${accountName})` : ""} by default
                </span>
                <span
                  className="flex-[0_0_.8rem] text-right font-bold text-accent"
                  aria-hidden="true"
                >
                  {isDefault ? <CheckIcon /> : null}
                </span>
              </Menu.CheckboxItem>
            ) : null}
            <Menu.Item
              className={menuItemClasses}
              aria-current={!currentAccountId && !current ? "true" : undefined}
              onClick={() => onPick("")}
            >
              All accounts · All mail
            </Menu.Item>
            {auth.isPending ? (
              <p className="mx-2 my-1.5 text-xs/snug text-muted">Reading connected accounts…</p>
            ) : null}
            {auth.isError ? (
              <p className="mx-2 my-1.5 text-xs/snug text-muted" role="alert">
                Connected accounts could not be read.
              </p>
            ) : null}
            {accounts.length === 0 && !auth.isPending && !auth.isError ? (
              <FolderRows current={current} onPick={onPick} />
            ) : null}
            {accounts.map((account, index) => {
              const name = account.email || account.displayName;
              return (
                <Menu.Group
                  className={index ? "mt-1 border-t border-line pt-1" : ""}
                  key={account.id}
                >
                  <Menu.GroupLabel className="truncate px-2 pt-1 pb-0.5 text-xs font-bold text-muted">
                    {name}
                  </Menu.GroupLabel>
                  <FolderRows
                    accountId={account.id}
                    currentAccountId={currentAccountId}
                    current={current}
                    onPick={onPick}
                  />
                </Menu.Group>
              );
            })}
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  );
}
