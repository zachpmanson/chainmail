/** Account-neutral: Gmail resolves it against the open mailbox, not account index 0. */
export function gmailMessageURL(gmailId: string): string {
  return `https://mail.google.com/mail/#all/${encodeURIComponent(gmailId)}`;
}
