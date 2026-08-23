/**
 * trimBody — strip whitespace-only nodes from the exposed edges of a message
 * body, right before it is rendered.
 *
 * This is the frontend (render-time) twin of the backend trimEdgeWhitespace.
 * It exists so pages whose bodies were baked before the backend trim shipped
 * still get their edges fixed on load, without a corpus re-cook. The two share
 * their contract:
 *
 *   - A span "has content" if it holds non-whitespace text, an image, or a
 *     folded signature. Whitespace means spaces, tabs, newlines, or a
 *     non-breaking space.
 *   - Leading spans with no content are dropped, but never a <pre>: its edge
 *     whitespace is the author's own.
 *   - Trailing spans with no content are dropped. If a folded signature
 *     (<details class="sig">) closes the body, it is kept (it is content) and
 *     the blanks between the last real line and the fold are dropped too, so
 *     the exposed remainder sits edge-on to the disclosure. The fold and its
 *     own internal padding are left untouched.
 *
 * Bodies reach this function as the serialized markup the backend already
 * produced, so they are balanced. The trim works on that serialized string:
 * it locates the top-level sibling spans, tells whether each holds content,
 * and keeps only the ones it means to. Survivors are spliced out of the
 * original string verbatim, so nothing is rewritten and the trim is
 * idempotent — a body the backend already trimmed passes through unchanged.
 */

const VOID = new Set([
  "area", "base", "br", "col", "embed", "hr", "img", "input",
  "link", "meta", "param", "source", "track", "wbr",
]);

/** Whitespace-only text: spaces, tabs, newlines, nbsp, and companions. */
const WS_RE = /^[\s\u00a0\u2007\u202f]*$/;
const ws = (s: string) => WS_RE.test(s);

interface Tag {
  /** lowercase element name */
  cls: string;
  classes: string;
  closing: boolean;
  selfClose: boolean;
  /** index just past the closing ">" */
  gt: number;
}

/** Parse one tag beginning at `lt` (which must be '<'). Null if the '<' starts
 *  a comment, doctype, cdata, or otherwise not an element tag we own. */
function parseTag(body: string, lt: number): Tag | null {
  if (body[lt] !== "<") return null;
  let j = lt + 1;
  const c = body[j];
  if (c === "!" || c === "?") return null;
  const closing = c === "/";
  if (closing) j++;
  while (j < body.length && ws(body[j]!)) j++;
  const ns = j;
  while (j < body.length && /[A-Za-z0-9-]/.test(body[j]!)) j++;
  const name = body.slice(ns, j);
  if (!name) return null;
  // scan attributes to the ">" honouring quotes
  let k = j;
  let quote = "";
  while (k < body.length) {
    const ch = body[k];
    if (quote) { if (ch === quote) quote = ""; }
    else if (ch === '"' || ch === "'") quote = ch;
    else if (ch === ">") break;
    k++;
  }
  if (k >= body.length) return null;
  const attrs = body.slice(j, k);
  let classes = "";
  const cm = /class\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))/.exec(attrs);
  if (cm) classes = (cm[2]! ?? cm[3]! ?? cm[4]! ?? "").trim();
  const tail = attrs.trim();
  const selfClose = closing ? false : /\/\s*$/.test(tail) || VOID.has(name);
  return { cls: name.toLowerCase(), classes, closing, selfClose, gt: k + 1 };
}

/** Whether a subtree (a top-level span) holds real content: an image, a <pre>
 *  (its edge whitespace is the author's), or any visible text after tags and
 *  whitespace entities are removed. */
function subtreeHasContent(body: string, start: number, end: number): boolean {
  const frag = body.slice(start, end);
  // images count as content
  if (/<img\b/i.test(frag)) return true;
  if (/<(pre|code|textarea)\b/i.test(frag)) return true;
  // remove tags and decode-agnostic whitespace entities, then look for visible
  const visible = frag
    .replace(/<[^>]*>/g, "")
    .replace(/&nbsp;|&#160;|&ensp;|&emsp;|&#8194;|&#8195;|&#8201;/gi, " ")
    .replace(/\s/g, "");
  return visible !== "";
}

/** Split a balanced body into top-level siblings: [start,end) spans plus a
 *  flag for each whether it is the signature fold. An unbalanced tail is kept
 *  as one span so no content is ever dropped. */
function topLevelSpans(body: string): { start: number; end: number; fold: boolean }[] {
  const out: { start: number; end: number; fold: boolean }[] = [];
  const push = (start: number, end: number, fold: boolean) => {
    if (end > start) out.push({ start, end, fold });
  };
  let i = 0;
  const len = body.length;
  while (i < len) {
    const lt = body.indexOf("<", i);
    if (lt === -1) { push(i, len, false); break; }
    if (lt > i) { push(i, lt, false); i = lt; continue; }
    const t = parseTag(body, lt);
    if (!t) { i++; continue; }
    if (t.closing) { i = t.gt; continue; } // stray close; skip
    if (t.selfClose) { push(lt, t.gt, false); i = t.gt; continue; }
    // find matching close for this element name
    let depth = 1;
    let j = t.gt;
    let end = -1;
    while (j < len) {
      const lt2 = body.indexOf("<", j);
      if (lt2 === -1) break;
      const t2 = parseTag(body, lt2);
      if (!t2) { j = lt2 + 1; continue; }
      if (t2.closing && t2.cls === t.cls) {
        depth--;
        if (depth === 0) { end = t2.gt; break; }
      } else if (!t2.closing && !t2.selfClose && t2.cls === t.cls) {
        depth++;
      }
      j = t2.gt;
    }
    if (end === -1) { push(lt, len, false); break; }
    const fold = t.cls === "details" && hasClass(t.classes, "sig");
    push(lt, end, fold);
    i = end;
  }
  return out;
}

function hasClass(classes: string, want: string): boolean {
  return classes.split(/\s+/).includes(want);
}

/** Trim the whitespace-only edges of a serialized message body. */
export function trimBody(body: string): string {
  const spans = topLevelSpans(body);
  if (spans.length === 0) return body;

  // A span is content if it is the fold or holds visible text/an image.
  const content = (i: number): boolean =>
    spans[i]!.fold || subtreeHasContent(body, spans[i]!.start, spans[i]!.end);
  const n = spans.length;

  // Leading: drop blank spans up to the first content (or fold).
  let first = 0;
  while (first < n && !content(first)) first++;

  // Trailing: find the last content span. When the body ends in a fold, the
  // fold is content and is handled as the very last kept span; blanks between
  // the last real line and it are dropped too (they sit above the disclosure,
  // not between lines).
  const endFold = spans[n - 1]!.fold;

  // When the body is exactly the fold (nothing else), keep just it.
  if (n === 1) return body;

  // Walk back from before the trailing fold (or from the end) to the last
  // content span, skipping the blank run that sits above the fold.
  let last = endFold ? n - 2 : n - 1;
  while (last >= first && !content(last)) last--;

  // Only a fold is content: keep just the fold.
  const lastContent = last;
  if (lastContent < first) {
    return endFold ? body.slice(spans[n - 1]!.start, spans[n - 1]!.end) : "";
  }

  if (!endFold && first === 0 && lastContent === n - 1) return body;

  // Keep every span from first..lastContent. Between two content spans a blank
  // is the author's separator and stays; the only blanks that fall out are the
  // leading run (before first) and the trailing run/above-the-fold run (after
  // lastContent), which we simply never enter.
  const parts: string[] = [];
  for (let i = first; i <= lastContent; i++) {
    const s = spans[i]!;
    parts.push(body.slice(s.start, s.end));
  }
  if (endFold) {
    const fold = spans[n - 1]!;
    parts.push(body.slice(fold.start, fold.end));
  }
  return parts.join("");
}