import { Button } from "./controls";
import { useEffect, useRef, useState } from "react";
import { CheckIcon, ChevronDownIcon } from "@heroicons/react/24/outline";
import { $api } from "../lib/api";

function FolderRows({
  accountId,
  currentAccountId,
  current,
  selectingDefault,
  onPick,
}: {
  accountId?: string;
  currentAccountId?: string;
  current: string;
  selectingDefault: boolean;
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
      {folders.isPending ? <p className="mx-[.45rem] my-[.35rem] text-[.72rem] leading-snug text-[var(--muted)]">Reading folders…</p> : null}
      {folders.isError ? <p className="mx-[.45rem] my-[.35rem] text-[.72rem] leading-snug text-[var(--muted)]" role="alert">The folder list could not be read.</p> : null}
      <Button
        type="button"
        role="menuitem"
        variant="menu"
        className={`w-full justify-start gap-2 rounded-md px-[.45rem] py-[.3rem] text-left text-[.78rem] font-normal aria-[current=true]:font-semibold ${accountId ? "pl-4" : ""}`}
        aria-current={selectedAccount && !current ? "true" : undefined}
        onClick={() => pick("")}
      >
        All mail
      </Button>
      {labels.map((label) => {
        const selected = selectedAccount && label.name === current;
        return (
          <Button
            key={label.name}
            type="button"
            role="menuitem"
            aria-current={selected ? "true" : undefined}
            variant="menu"
            className={`w-full justify-start gap-2 rounded-md px-[.45rem] py-[.3rem] text-left text-[.78rem] font-normal aria-[current=true]:font-semibold ${accountId ? "pl-4" : ""}`}
            onClick={() => pick(label.name)}
          >
            <span className="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap">{label.name}</span>
            <span className="text-[.7rem] tabular-nums text-[var(--muted)]">{label.messages}</span>
          </Button>
        );
      })}
      {!folders.isPending && !folders.isError && labels.length === 0 ? (
        <p className="mx-[.45rem] my-[.35rem] text-[.72rem] leading-snug text-[var(--muted)]">
          No message carries a label yet — the mailbox's own labels are what this list is,
          so it is empty rather than invented.
        </p>
      ) : null}
    </>
  );
}

/** The shared account-grouped folder dropdown used by the inbox and its default-folder setting. */
export function FolderPicker({
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
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement | null>(null);
  const accounts = (auth.data?.accounts ?? []).filter((account) => account.signedIn);
  const currentAccount = accounts.find((account) => account.id === currentAccountId);
  const accountName = currentAccount?.email || currentAccount?.displayName || currentAccountId;
  const selectingDefault = mode === "default";

  useEffect(() => {
    if (!open) return;
    const away = (ev: MouseEvent) => {
      if (box.current && !box.current.contains(ev.target as Node)) setOpen(false);
    };
    const esc = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", away);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", away);
      document.removeEventListener("keydown", esc);
    };
  }, [open]);

  const pick = (name: string, accountId?: string) => {
    setOpen(false);
    onPick(name, accountId);
  };

  return (
    <div className="relative z-30 m-0" ref={box}>
      <Button
        type="button"
        variant="secondary"
        className="w-full justify-start gap-[.4rem] px-[.55rem] py-[.35rem] text-left text-xs font-semibold hover:border-[var(--muted)]"
        aria-label={ariaLabel}
        aria-haspopup="menu"
        disabled={disabled}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap">
          <span className="ibfbtn-folder font-bold">{current || "All mail"}</span>
          {currentAccountId ? (
            <>
              {" "}
              <span className="ibfbtn-account font-normal text-[var(--muted)]">({accountName})</span>
            </>
          ) : null}
        </span>
        <ChevronDownIcon className="h-3 w-3 shrink-0 text-[var(--muted)]" aria-hidden="true" />
      </Button>

      {open ? (
        <div className="absolute left-0 right-0 top-[calc(100%+.25rem)] z-[31] max-h-[22rem] overflow-y-auto rounded-lg border border-[var(--line)] bg-[var(--card)] p-1 shadow-[0_8px_24px_rgba(0,0,0,.22)]" role="menu" aria-label="Folders">
          {!selectingDefault ? (
            <Button
              type="button"
              role="menuitemcheckbox"
              aria-checked={isDefault}
              variant="menu"
              className="w-full justify-start gap-2 rounded-md px-[.45rem] py-[.3rem] text-left text-[.78rem] font-normal"
              onClick={() => onDefault?.(!isDefault)}
            >
              <span className="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap">
                Open {current || "All mail"}{currentAccountId && accountName ? ` (${accountName})` : ""} by default
              </span>
              <span className="ibfmark flex-[0_0_.8rem] text-right font-bold text-accent" aria-hidden="true">{isDefault ? <CheckIcon /> : null}</span>
            </Button>
          ) : null}
          <Button
            type="button"
            role="menuitem"
            variant="menu"
            className="w-full justify-start gap-2 rounded-md px-[.45rem] py-[.3rem] text-left text-[.78rem] font-normal aria-[current=true]:font-semibold"
            aria-current={!currentAccountId && !current ? "true" : undefined}
            onClick={() => pick("")}
          >
            All accounts · All mail
          </Button>
          {auth.isPending ? <p className="mx-[.45rem] my-[.35rem] text-[.72rem] leading-snug text-[var(--muted)]">Reading connected accounts…</p> : null}
          {auth.isError ? <p className="mx-[.45rem] my-[.35rem] text-[.72rem] leading-snug text-[var(--muted)]" role="alert">Connected accounts could not be read.</p> : null}
          {accounts.length === 0 && !auth.isPending && !auth.isError ? (
            <FolderRows current={current} selectingDefault={selectingDefault} onPick={pick} />
          ) : null}
          {accounts.map((account, index) => {
            const name = account.email || account.displayName;
            return (
              <div className={index ? "mt-[.3rem] border-t border-[var(--line)] pt-[.3rem]" : ""} role="group" aria-label={name} key={account.id}>
                <div className="overflow-hidden text-ellipsis whitespace-nowrap px-[.45rem] pt-[.25rem] pb-[.15rem] text-[.7rem] font-bold text-[var(--muted)]">{name}</div>
                <FolderRows
                  accountId={account.id}
                  currentAccountId={currentAccountId}
                  current={current}
                  selectingDefault={selectingDefault}
                  onPick={pick}
                />
              </div>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
