import type { MediaPull } from "./api";
import type { Entry } from "./spec";

export type Attachment = NonNullable<Entry["attachments"]>[number];

/**
 * Where the app serves bytes the corpus holds: GET /v1/attachments/{sha}.
 *
 * The static export passes no base at all rather than this constant, which is how
 * a shared page keeps pointing at Gmail — it is a file, not a reader, and there is
 * no server behind it that could answer.
 */
export const MEDIA_BASE = "/v1/attachments";

/**
 * The local copy's URL, where the corpus holds this attachment's bytes and the
 * renderer has somewhere to serve them from. Undefined otherwise — the digest
 * without a base is a static export, and the base without a digest is a file that
 * was never pulled.
 */
export function localHref(a: Attachment, mediaBase: string): string | undefined {
  return mediaBase && a.blobSha ? `${mediaBase}/${a.blobSha}` : undefined;
}

/**
 * Where an attachment opens, or undefined when nothing can open it.
 *
 * Local bytes win over every source link, and that is the point of pulling a file
 * into the corpus: a reader stops leaving the page for something the page already
 * has. A stored file is served inline or as a download according to the `open`
 * rule the server decided it by, so what the click does is not the page's to
 * guess — see nothing here that branches on the type.
 *
 * Gmail then wins over the source link because a mail attachment is reached through
 * its message, and the message is the more useful place to land: it carries the
 * thread the file arrived in. Slack records a permalink per file and has no
 * equivalent, so it uses that. An attachment recovered from quoted text has
 * neither, and stays a label — there is nowhere honest to send the reader.
 */
export function attHref(a: Attachment, mediaBase = ""): string | undefined {
  const local = localHref(a, mediaBase);
  if (local) return local;
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

/**
 * Whether the corpus has decided it will never hold these bytes.
 *
 * Distinct from "not fetched yet": a skip is a recorded answer, so there is
 * nothing left to ask for. The page uses this both to explain the chip and to
 * leave the fetch button off a message whose files are all accounted for.
 */
export function isSkipped(a: Attachment): boolean {
  return Boolean(a.skip);
}

/**
 * Why a file is not in the corpus, in words.
 *
 * The wire carries the corpus's own word so the same state reads the same way in
 * the CLI report as it does here; the sentence belongs to the page, because a
 * chip has to say this in a few words and a log has room for more. An unknown
 * value — a corpus newer than this client — is reported as what it is rather than
 * guessed at.
 */
export function skipNote(a: Attachment): string | undefined {
  if (!a.skip) return undefined;
  switch (a.skip) {
    case "too_large":
      return "not stored: larger than the fetch cap";
    case "unavailable":
      return "not stored: empty in the message";
    case "part_not_found":
      return "not stored: not in the message any more";
    case "no_source_ref":
      return "not stored: nothing to fetch it by";
    case "no_message_id":
      return "not stored: no mailbox message to ask";
    case "no_bytes":
      return "not stored: missing from the archive";
    default:
      return `not stored (${a.skip})`;
  }
}

/**
 * One line saying what a pull did. The response carries counts and one row per
 * file, and the rows are the part a person needs: "nothing was fetched" is not
 * an answer when one file was too large and another has moved out of the
 * sender's mailbox. Console-only, like the refresh verdict — the surface's job is
 * the surface, and a chip that became a picture says the rest.
 *
 * Shared by the two places a pull can be pressed (the built page and the reading
 * pane), because one account of one operation written twice is how two renderers
 * come to say different things about the same fetch.
 */
export function pullSummary(media: MediaPull): string {
  if (!media.wanted) return "no files needed fetching";
  const parts = [`${media.pulled}/${media.wanted} files fetched`];
  const declined = media.files.filter((f) => f.reason);
  if (declined.length)
    parts.push(
      `${declined.length} declined (${declined.map((f) => `${f.name}: ${f.reason}`).join(", ")})`,
    );
  const failed = media.files.filter((f) => f.error);
  if (failed.length)
    parts.push(
      `${failed.length} failed, will retry (${failed.map((f) => `${f.name}: ${f.error}`).join(", ")})`,
    );
  return parts.join(", ");
}
