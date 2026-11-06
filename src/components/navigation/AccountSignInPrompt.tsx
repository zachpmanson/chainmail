import { $api } from "../../lib/api/api";
import ButtonLink from "../ui/ButtonLink";

export default function AccountSignInPrompt() {
  const auth = $api.useQuery("get", "/auth/status", {});
  if (
    !auth.data ||
    auth.isPending ||
    auth.isError ||
    auth.data.signed_in ||
    auth.data.accounts.some((account) => account.signedIn)
  )
    return null;

  return (
    <section
      aria-label="Connect a Gmail account"
      className="flex flex-wrap items-center justify-center gap-3 border-b border-line px-5 py-3"
    >
      <p className="m-0 text-sm text-muted">Connect a Gmail account to start syncing your mail.</p>
      <ButtonLink variant="primary" href="/auth/login">
        Sign in with Google
      </ButtonLink>
    </section>
  );
}
