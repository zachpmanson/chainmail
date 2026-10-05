/** Gmail's active-account URL, avoiding the incorrect assumption that every
 * message belongs to the browser's account index zero. Gmail resolves this
 * account-neutral path against the mailbox the reader currently has open. */
export function gmailMessageURL(gmailId: string): string {
  return `https://mail.google.com/mail/#all/${encodeURIComponent(gmailId)}`;
}
