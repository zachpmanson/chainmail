import SelectInput from "../ui/SelectInput";
import useAccounts from "./useAccounts";

export default function AccountSelect({
  value,
  disabled,
  onChange,
  alwaysOfferNone = false,
}: {
  value: string;
  disabled: boolean;
  onChange: (accountId: string) => void;
  /** Keep "Choose account" listed after one is picked. */
  alwaysOfferNone?: boolean;
}) {
  const { connected } = useAccounts();
  return (
    <SelectInput
      className="h-7 min-w-0 flex-1 rounded-md border border-line bg-bg px-1 text-xs text-fg focus:border-accent disabled:cursor-default disabled:opacity-55"
      aria-label="From"
      value={value}
      disabled={disabled}
      onChange={(event) => onChange(event.target.value)}
    >
      {alwaysOfferNone || !value ? <option value="">Choose account</option> : null}
      {connected.map((account) => (
        <option key={account.id} value={account.id}>
          {account.displayName}
          {account.email ? ` (${account.email})` : ""}
        </option>
      ))}
    </SelectInput>
  );
}
