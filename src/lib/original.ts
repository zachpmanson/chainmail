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

/**
 * Which senders the reader reads as their sender wrote them — this browser's own
 * copy of that answer, and the whole of it for a caller with no corpus to ask.
 *
 * The store of record is the corpus, not here: the answer belongs to the person
 * the sender resolves to (the corpus's people.prefer_original), it is served on
 * every entry as `preferOriginal`, and it is written back with POST
 * /v1/people/{personId} — see useOriginal in components/Message.tsx, and the pane
 * that makes the write. Storing it there rather than here is what makes "read
 * Ada's mail as Ada wrote it" true on the reader's phone as well as on this
 * browser, and what stops an identity merge of two spellings of one person from
 * forgetting which way their mail is read.
 *
 * What is left here is the fallback, for the two callers the corpus cannot answer
 * for. A built page has no server behind it at all, and its control still has to
 * do something. And a message with no sender to ask about — one recovered from
 * somebody else's quote — has nobody for the answer to be about, so the message is
 * what is remembered instead.
 *
 * The switch is per sender and not per message, because the mail that needs it
 * is mail that was never meant for this pipeline: a booking confirmation, a
 * newsletter, anything a script sent. Those arrive from one address and arrive
 * as a series, and a reader who has decided that GitHub's notifications are
 * illegible should not have to say so again on every one of them.
 *
 * Held in localStorage rather than in the session, for the same reason the
 * decision is about the sender rather than the message: the next slurp brings
 * more of their mail, and the reader's answer to "how do I read this person" is
 * not something to re-answer every time the corpus grows. Keyed by the address
 * the entry came from (`fromEmail`), which is the only handle a sender has here:
 * the display name is neither unique nor always present.
 *
 * A message with no address of its own — one recovered from somebody else's
 * quote — has no sender to hold the answer, so the caller keys it on the message
 * instead (the `key` useOriginal builds), and it is the message that is
 * remembered, not a person the corpus cannot name.
 */
const STYLED_KEY = "chainmail:styled-senders";

let styled: Set<string> | null = null;
const watchers = new Set<() => void>();

function store(): Set<string> {
  if (styled) return styled;
  styled = new Set<string>();
  try {
    const held = globalThis.localStorage?.getItem(STYLED_KEY);
    if (held) for (const key of JSON.parse(held) as unknown[]) {
      if (typeof key === "string" && key !== "") styled.add(key);
    }
  } catch {
    // No storage, or somebody else's key under this name: an unreadable answer
    // is the same as no answer, and the reader starts from the default rather
    // than from a broken page.
  }
  return styled;
}

function save(): void {
  try {
    globalThis.localStorage?.setItem(STYLED_KEY, JSON.stringify([...store()]));
  } catch {
    // The switch still holds for this session; it just will not be remembered.
  }
}

/** Whether the reader reads this sender's mail as the sender wrote it. */
export function isStyled(key: string): boolean {
  return key !== "" && store().has(key);
}

/**
 * Flip one sender's switch, and tell every bubble that asked about it.
 *
 * The notification is the point of this being here rather than in the bubble: a
 * sender's mail is usually a run of messages, all of them mounted, and pressing
 * the control on one of them has to reach the others. Who is listening is the
 * bubble's business — this only knows that something changed.
 */
export function toggleStyled(key: string): void {
  if (key === "") return;
  const held = store();
  if (held.has(key)) held.delete(key);
  else held.add(key);
  save();
  for (const w of [...watchers]) w();
}

/** Listen for any sender's switch moving. Returns the unsubscribe. */
export function watchStyled(fn: () => void): () => void {
  watchers.add(fn);
  return () => {
    watchers.delete(fn);
  };
}

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
  dropSchemeVariants(root);
}

/**
 * Take the sender's colour-scheme variants out of their own stylesheets.
 *
 * A mail that writes `@media (prefers-color-scheme: dark)` is answering a
 * question about the canvas it has been drawn on, and the answer this app gets is
 * the wrong one. The mail is mounted on the app's paper — white, black ink,
 * `color-scheme:light` (see appCanvas in internal/spec/original.go) — and inside a
 * shadow root the media query is answered by the *document*, not by the host's
 * `color-scheme`: the app's own theme is the same query (see styles.css), so in
 * dark mode the sender's dark rules apply and the sender's paper stays white.
 *
 * Measured on a Google Calendar invitation, whose dark block is
 * `color:#e8eaed !important` on its body text and `#e8eaed` on the two classes
 * beside it: mounted in dark mode the ink came out `rgb(232,234,237)` over
 * `rgb(255,255,255)` — 1.1:1, which is a mail nobody can read — and the same
 * mount with `color-scheme: only light` in the canvas came out identically, so
 * this is not something the canvas can win. That mail is not unusual: of the
 * senders this corpus holds, forty write a colour-scheme variant, and the shape
 * is always the same one — the dark block restates the *ink* and leaves the paper
 * to the client, because the client it was written for darkens the paper.
 *
 * So the variant is not applied, and the mail is drawn once: as its author wrote
 * it for the paper it is actually on. That is the same preference the canvas
 * already states one layer down — the sender's design over the app's, and the
 * sender's *light* design over a dark one this app has no way to draw.
 *
 * Only `prefers-color-scheme`, and the narrowness is deliberate: the other
 * conditional modes a mail can ask about (`forced-colors`, `prefers-contrast`, a
 * width query) are statements about the reader's own display and mean what they
 * say there, and dropping those would be losing an accessibility mode to fix a
 * colour. Nested rules are walked, because a colour-scheme block inside a width
 * query is still a colour-scheme block.
 *
 * The parameter is what it needs and no more — something with stylesheets — so
 * that the walk can be reached by a test: the shadow root's own `styleSheets` is
 * not implemented everywhere this app runs (jsdom has none), and a rule about
 * which of a sender's styles apply is worth testing for real rather than
 * trusting.
 */
export function dropSchemeVariants(root: { styleSheets?: ArrayLike<CSSStyleSheet> | null }): void {
  for (const sheet of Array.from(root.styleSheets ?? [])) dropFrom(sheet);
}

function dropFrom(group: CSSStyleSheet | CSSGroupingRule): void {
  const rules = group.cssRules;
  // Backwards: deleting a rule shifts every rule after it.
  for (let i = rules.length - 1; i >= 0; i--) {
    // Duck-typed rather than `instanceof CSSMediaRule`, because what is being
    // asked for is a rule with a condition and a body — which is also what
    // `@supports` is — and this walk should not depend on a constructor.
    const rule = rules[i] as CSSRule & { cssRules?: CSSRuleList; conditionText?: string };
    if (!rule.cssRules) continue;
    if (/prefers-color-scheme/i.test(rule.conditionText ?? "")) {
      group.deleteRule(i);
      continue;
    }
    dropFrom(rule as unknown as CSSGroupingRule);
  }
}