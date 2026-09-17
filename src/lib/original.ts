import { ApiError } from "./api";

/**
 * The reader's second reading of a message: the html its sender wrote, fetched
 * from the corpus and mounted in a shadow root of its own.
 *
 * Two things happen here, and neither belongs in the component that draws a
 * bubble: the fetch (one route, one shape of failure) and the mount (a shadow
 * root, which is DOM rather than React). The bubble decides *when* a message is
 * shown this way; this decides *how*, so the reading pane and any later caller
 * get the same second rendering rather than each deriving one.
 *
 * What this is not is the security boundary. The html arrives sanitised by the
 * server (spec.OriginalBody) — a shadow root is encapsulation, not isolation: it
 * carries no sandbox attribute and no CSP of its own, so script that reaches it
 * runs beside the app.
 */

/** Where one message's own html is served: GET /v1/entries/{extId}/original.
 *  The ext id is the whole address — the corpus's handle for the message, which
 *  is the same one its attachments and its read are asked for by. */
export const ORIGINAL_BASE = "/v1/entries";

/**
 * One message's html as its sender wrote it.
 *
 * Held for the session, keyed by ext id, as the promise rather than the bytes:
 * flipping back and forth between the two renderings is what a reader does with
 * this control, and paying a 40 KB round trip on every flip would make the flip
 * the slow part. The server sends no-cache (the corpus is rewritten by every
 * slurp, so a held copy is a held copy of older mail) and this is not that: it
 * is the same session the reader is reading in, and nothing here outlives it.
 *
 * A rejection is NOT held. "No html part on this one" is a fact about the
 * message, but a failure to reach the service is a fact about the moment, and a
 * cached rejection would make one bad second the answer for the rest of the
 * session.
 */
const asked = new Map<string, Promise<string>>();

export function fetchOriginal(extID: string): Promise<string> {
  const held = asked.get(extID);
  if (held) return held;
  const fetching = ask(extID);
  asked.set(extID, fetching);
  fetching.catch(() => asked.delete(extID));
  return fetching;
}

async function ask(extID: string): Promise<string> {
  const res = await fetch(`${ORIGINAL_BASE}/${encodeURIComponent(extID)}/original`);
  if (!res.ok) throw new ApiError(res.status, await whyNot(res));
  return res.text();
}

/**
 * The server's own sentence, or a true one about the status when it sent none.
 *
 * The failures here are informative — "this entry carries no text/html part of
 * its own" is the whole answer, and "not found" would not be — so the message is
 * taken from the body the same way the API client takes it. Duplicated in
 * miniature rather than reached for: this route is fetched outside the generated
 * client (its response is a document, not a resource the typed hooks can hold),
 * and the one thing that must not differ is what the reader is told.
 */
async function whyNot(res: Response): Promise<string> {
  const body = await res.text();
  try {
    const parsed = JSON.parse(body) as { error?: unknown };
    if (typeof parsed.error === "string" && parsed.error.trim() !== "") return parsed.error;
  } catch {
    // Not JSON: whatever arrived is the message.
  }
  const text = body.trim();
  if (text !== "") return text;
  return res.statusText.trim() !== "" ? `${res.status} ${res.statusText}` : `HTTP ${res.status}`;
}

/**
 * Put a message's html inside a shadow root of `host`.
 *
 * A shadow root rather than an iframe, because the two things that make the
 * sender's mail legible are a stylesheet and the class names that address it, and
 * a shadow tree contains both without rewriting either: styles cross the boundary
 * in neither direction, so the mail cannot restyle the app and the app cannot
 * restyle the mail. An iframe would do the same and cost a viewport — a fixed
 * height, or a measurement pass to escape it.
 *
 * The root is created once per host and refilled on every mount, because
 * attachShadow throws if it is called twice and a reader who flips back and forth
 * re-enters a root their browser is still holding.
 */
export function mountOriginal(host: HTMLElement, html: string): void {
  const root = host.shadowRoot ?? host.attachShadow({ mode: "open" });
  root.innerHTML = html;
}