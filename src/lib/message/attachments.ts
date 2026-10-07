import type { MediaPull } from "../api/api";
import type { Entry } from "../timeline/spec";
import { gmailMessageURL } from "./gmailUrl";

export type Attachment = NonNullable<Entry["attachments"]>[number];

// The static export passes no base, so shared pages link to Gmail instead.
export const MEDIA_BASE = "/v1/attachments";

export function localHref(a: Attachment, mediaBase: string): string | undefined {
  return mediaBase && a.blobSha ? `${mediaBase}/${a.blobSha}` : undefined;
}

/** Local bytes, then the Gmail message, then Slack's permalink. Quote-recovered files have none. */
export function attHref(a: Attachment, mediaBase = ""): string | undefined {
  const local = localHref(a, mediaBase);
  if (local) return local;
  if (a.gmailId) return gmailMessageURL(a.gmailId);
  return a.link || undefined;
}

/** The spec builder decides what gets a preview; don't second-guess it here. */
export function hasPreview(a: Attachment): a is Attachment & { preview: string } {
  return typeof a.preview === "string" && a.preview.startsWith("data:image/");
}

// `kind` is the fallback for bytes pulled before `view` existed.
export function isPicture(a: Attachment): boolean {
  return a.view === "image" || (a.kind ?? "").toLowerCase() === "image";
}

/**
 * Prefers the embedded thumbnail (no request, and keeps the popover's 404 fallback in
 * behaviour.ts), then the pulled bytes. `blob` marks the latter, which have no known width.
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

export function isSkipped(a: Attachment): boolean {
  return Boolean(a.skip);
}

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
