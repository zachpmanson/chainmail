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
      {folders.isPending ? <p className="ibfnote">Reading folders…</p> : null}
      {folders.isError ? <p className="ibfnote" role="alert">The folder list could not be read.</p> : null}
      <button
        type="button"
        role="menuitem"
        className={`ibfrow${selectedAccount && !current ? " sel" : ""}`}
        aria-current={selectedAccount && !current ? "true" : undefined}
        onClick={() => pick("")}
      >
        All mail
      </button>
      {labels.map((label) => {
        const selected = selectingDefault ? label.name === current : selectedAccount && label.name === current;
        return (
          <button
            key={label.name}
            type="button"
            role="menuitem"
            aria-current={selected ? "true" : undefined}
            className={`ibfrow${selected ? " sel" : ""}`}
            onClick={() => pick(label.name)}
          >
            <span className="ibfname">{label.name}</span>
            <span className="ibfcount">{label.messages}</span>
          </button>
        );
      })}
      {!folders.isPending && !folders.isError && labels.length === 0 ? (
        <p className="ibfnote">
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
    <div className="ibfolders" ref={box}>
      <button
        type="button"
        className="ibfbtn"
        aria-label={ariaLabel}
        aria-haspopup="menu"
        disabled={disabled}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="ibfbtn-label">
          <span className="ibfbtn-folder">{current || "All mail"}</span>
          {currentAccountId ? (
            <>
              {" "}
              <span className="ibfbtn-account">({accountName})</span>
            </>
          ) : null}
        </span>
        <ChevronDownIcon className="ibfcaret" aria-hidden="true" />
      </button>

      {open ? (
        <div className="ibpop" role="menu" aria-label="Folders">
          {!selectingDefault && !currentAccountId ? (
            <button
              type="button"
              role="menuitemcheckbox"
              aria-checked={isDefault}
              className="ibfrow ibfdefault"
              onClick={() => onDefault?.(!isDefault)}
            >
              <span className="ibfname">Open {current || "All mail"} by default</span>
              <span className="ibfmark" aria-hidden="true">{isDefault ? <CheckIcon /> : null}</span>
            </button>
          ) : null}
          <button
            type="button"
            role="menuitem"
            className={`ibfrow${!currentAccountId && !current ? " sel" : ""}`}
            aria-current={!currentAccountId && !current ? "true" : undefined}
            onClick={() => pick("")}
          >
            All accounts · All mail
          </button>
          {auth.isPending ? <p className="ibfnote">Reading connected accounts…</p> : null}
          {auth.isError ? <p className="ibfnote" role="alert">Connected accounts could not be read.</p> : null}
          {accounts.length === 0 && !auth.isPending && !auth.isError ? (
            <FolderRows current={current} selectingDefault={selectingDefault} onPick={pick} />
          ) : null}
          {accounts.map((account) => {
            const name = account.email || account.displayName;
            return (
              <div className="ibfgroup" role="group" aria-label={name} key={account.id}>
                <div className="ibfheading">{name}</div>
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
