import { $api } from "../lib/api";
import { Button, ControlLink } from "./controls";
import { SettingsSection } from "./SettingsScaffold";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/** Mailbox connection and disconnection controls, including their confirmation boundary. */
export function SettingsAccounts() {
  const auth = $api.useQuery("get", "/auth/status", {});
  const disconnect = $api.useMutation("post", "/auth/accounts/{accountId}/disconnect", {
    onSuccess: () => auth.refetch(),
  });
  const connected = (auth.data?.accounts ?? []).filter((account) => account.signedIn);

  return (
    <SettingsSection
      id="gmail-accounts-heading"
      title="Gmail accounts"
      description="Connect the mailboxes Chainmail syncs. Disconnecting keeps already imported mail."
    >
      {auth.isError ? (
        <p
          className="stmessage-error mt-[.65rem] rounded-sm border-l-[3px] border-l-red-700 bg-[var(--bg)] px-[.65rem] py-2 text-[.76rem] leading-[1.45] text-[var(--fg)] break-words mb-0"
          role="alert"
        >
          {errText(auth.error)}
        </p>
      ) : auth.isPending ? (
        <p className="mt-[.65rem] mb-0 text-[.76rem] leading-[1.45] text-[var(--muted)]">
          Checking connected accounts…
        </p>
      ) : (
        <>
          {connected.length > 0 ? (
            <ul className="mb-[.7rem] mt-0 list-none border-t border-[var(--line)] p-0">
              {connected.map((account) => {
                const label = account.email || account.displayName;
                return (
                  <li
                    className="flex items-center justify-between gap-3 border-b border-[var(--line)] py-[.55rem] text-xs break-words"
                    key={account.id}
                  >
                    <span>{label}</span>
                    <Button
                      type="button"
                      variant="secondary"
                      aria-label={`Disconnect ${label}`}
                      disabled={disconnect.isPending}
                      onClick={() => {
                        const warning =
                          account.id === "legacy"
                            ? " This also removes the shared Docket token."
                            : "";
                        if (
                          window.confirm(
                            `Disconnect ${label}? Chainmail will stop syncing from this mailbox, but imported mail stays in the corpus.${warning}`,
                          )
                        ) {
                          disconnect.mutate({ params: { path: { accountId: account.id } } });
                        }
                      }}
                    >
                      Disconnect
                    </Button>
                  </li>
                );
              })}
            </ul>
          ) : (
            <p className="mt-[.65rem] mb-0 text-[.76rem] leading-[1.45] text-[var(--muted)]">
              No Gmail accounts connected. Mailbox syncing is paused.
            </p>
          )}
          {disconnect.isError ? (
            <p
              className="mt-[.65rem] rounded-sm border-l-[3px] border-l-red-700 bg-[var(--bg)] px-[.65rem] py-2 text-[.76rem] leading-[1.45] text-[var(--fg)] break-words mb-0"
              role="alert"
            >
              Could not disconnect: {errText(disconnect.error)}
            </p>
          ) : null}
          <ControlLink variant="primary" href="/auth/login">
            {connected.length === 0 ? "Sign in with Google" : "Connect another account"}
          </ControlLink>
        </>
      )}
    </SettingsSection>
  );
}
