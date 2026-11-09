/** A known mailbox email selects the account without assuming a browser index. */
export function gmailMessageURL(gmailId: string, accountEmail?: string): string {
  const query = accountEmail ? `?${new URLSearchParams({ authuser: accountEmail })}` : "";
  return `https://mail.google.com/mail/${query}#all/${encodeURIComponent(gmailId)}`;
}

/** Accept only Gmail message links, not prefix lookalikes or arbitrary redirects. */
export function isGmailMessageURL(link: string): boolean {
  try {
    const url = new URL(link);
    const keys = [...url.searchParams.keys()];
    return (
      url.origin === "https://mail.google.com" &&
      !url.username &&
      !url.password &&
      url.pathname === "/mail/" &&
      /^#all\/[^/?#]+$/.test(url.hash) &&
      (keys.length === 0 ||
        (keys.length === 1 && keys[0] === "authuser" && Boolean(url.searchParams.get("authuser"))))
    );
  } catch {
    return false;
  }
}

export function gmailAccountEmail(link?: string): string | undefined {
  return link && isGmailMessageURL(link)
    ? (new URL(link).searchParams.get("authuser") ?? undefined)
    : undefined;
}
