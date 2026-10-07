import { $api } from "../../lib/api/api";
import Button from "../ui/Button";
import ButtonLink from "../ui/ButtonLink";
import { errText } from "../../lib/ui/errText";
import SettingsSection from "./SettingsSection";
import InlineAlert from "../ui/InlineAlert";

/** Mailbox connection and disconnection controls, including their confirmation boundary. */
export default function SettingsAccounts() {
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
        <InlineAlert compact>{errText(auth.error)}</InlineAlert>
      ) : auth.isPending ? (
        <p className="mt-3 mb-0 text-xs/normal text-muted">Checking connected accounts…</p>
      ) : (
        <>
          {connected.length > 0 ? (
            <ul className="mt-0 mb-3 list-none border-t border-line p-0">
              {connected.map((account) => {
                const label = account.email || account.displayName;
                return (
                  <li
                    className="flex items-center justify-between gap-3 border-b border-line py-2 text-xs wrap-break-word"
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
            <p className="mt-3 mb-0 text-xs/normal text-muted">
              No Gmail accounts connected. Mailbox syncing is paused.
            </p>
          )}
          {disconnect.isError ? (
            <InlineAlert compact>Could not disconnect: {errText(disconnect.error)}</InlineAlert>
          ) : null}
          <ButtonLink variant="primary" href="/auth/login">
            {connected.length === 0 ? "Sign in with Google" : "Connect another account"}
          </ButtonLink>
        </>
      )}
    </SettingsSection>
  );
}
