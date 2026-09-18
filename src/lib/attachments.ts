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
 *
 * Typed as a guard so that a caller which has asked the question can use the
 * answer: the field is optional on the wire, and the check that it is a data URI
 * of an image is this function's, not every caller's again.
 */
export function hasPreview(a: Attachment): a is Attachment & { preview: string } {
  return typeof a.preview === "string" && a.preview.startsWith("data:image/");
}

/**
 * Whether these bytes are a picture, as the server read them.
 *
 * `view` is that reading of the stored MIME — the same decision that frames a
 * PDF and hands a zip over, kept in one place so the renderers cannot disagree
 * about a file. `kind` is the fallback for bytes pulled before views had names:
 * a corpus is pulled into live rather than rebuilt like a page, so a picture it
 * holds from under the old rules must still show as one.
 */
export function isPicture(a: Attachment): boolean {
  return a.view === "image" || (a.kind ?? "").toLowerCase() === "image";
}

/**
 * The picture a chip shows before anyone clicks it, and where it comes from.
 *
 * Two sources, and the embedded one comes first — the other order from attHref,
 * which prefers the bytes because the reader who clicks asked for the file. This
 * answers a different question: what the chip shows without being asked. A
 * thumbnail the builder embedded is already in the markup — no request, nothing to
 * wait for, sized by the fetch of the file it was made beside — and drawing it
 * leaves the popover's own fallback intact, which is the one place a picture with
 * two sources can still be shown when the first one 404s (see behaviour.ts).
 *
 * The bytes come second, for the case the other source cannot cover at all: a file
 * pulled into the live app. There is no builder in that path, so nothing embedded
 * the picture and nothing knows its width — a preview here is the corpus's own
 * copy, and it appears the moment the download finishes, which is what makes a
 * fetched file recognisable rather than a name on a chip.
 *
 * `blob` says which of the two this is, because the two want drawing differently:
 * a data URI carries the size it was embedded at (the builder knew the pixels),
 * while bytes have no width until they have arrived (see .athumb.ablob).
 *
 * Everything that is not a picture gets nothing. A PDF surfaces in the window and
 * is unreadable at chip height, text is read there too, and an archive can only be
 * taken — so the one thing left to draw a thumbnail from is a picture, and whether
 * this is one is the server's answer rather than the name's.
 */
export function thumbnail(
  a: Attachment,
  mediaBase = "",
): { src: string; blob: boolean; w?: number; h?: number } | undefined {
  if (hasPreview(a)) return { src: a.preview, blob: false, w: a.previewW, h: a.previewH };
  const local = localHref(a, mediaBase);
  if (local && isPicture(a)) return { src: local, blob: true };
  return undefined;
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
