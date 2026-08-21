import type { Entry } from "./spec";

export type Attachment = NonNullable<Entry["attachments"]>[number];

/**
 * Where an attachment opens at its source, or undefined when nothing can open it.
 *
 * Gmail wins over the source link because a mail attachment is reached through
 * its message, and the message is the more useful place to land: it carries the
 * thread the file arrived in. Slack records a permalink per file and has no
 * equivalent, so it uses that. An attachment recovered from quoted text has
 * neither, and stays a label — there is nowhere honest to send the reader.
 */
export function attHref(a: Attachment): string | undefined {
  if (a.gmailId) return `https://mail.google.com/mail/u/0/#all/${a.gmailId}`;
  return a.link || undefined;
}

/**
 * Whether this attachment carries a thumbnail to show.
 *
 * The decision of *what deserves* a preview is made where the bytes are, at
 * spec-build time, since it needs the decoded pixel dimensions. By the time the
 * page has the spec, the presence of the field is the whole answer — the page
 * must not second-guess it, or the two rules drift and a picture appears in one
 * renderer and not the other.
 */
export function hasPreview(a: Attachment): boolean {
  return typeof a.preview === "string" && a.preview.startsWith("data:image/");
}
