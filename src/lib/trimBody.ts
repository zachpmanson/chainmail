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

/** Whether a span is, after unwrapping thin single-element wrapper layers,
 *  a <details class="sig"> fold. Some clients wrap the stage: <div>
 *  <details class="sig">…</details></div>. The fold is that wrapper's whole
 *  point, so a wrapper that reduces to a sig-fold counts as the fold. */
function resolvesToFold(body: string, span: { start: number; end: number }): boolean {
  let start = span.start;
  let end = span.end;
  for (let depth = 0; depth < 8; depth++) {
    const inner = body.slice(start, end);
    const subs = topLevelSpans(inner);
    if (subs.length !== 1) return false;
    const only = subs[0]!;
    const tg = parseTag(inner, only.start);
    // in the given span an actual sig-details fold counts directly
    if (tg && !tg.closing && tg.cls === "details" && hasClass(tg.classes, "sig")) {
      return true;
    }
    // unwrap a single non-void, non-pre element covering the whole span
    if (
      tg &&
      !tg.closing &&
      !tg.selfClose &&
      tg.cls !== "pre" &&
      only!.start === 0 &&
      only!.end === inner.length
    ) {
      start += tg.gt; // past the opening tag
      end -= tg.cls.length + 3; // before the matching close </name>
      continue;
    }
    return false;
  }
  return false;
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
  if (first === n) return "";

  // Trailing: find the last content span. When the body ends in a fold, the
  // fold is content and is handled as the very last kept span; blanks between
  // the last real line and it are dropped too (they sit above the disclosure,
  // not between lines).
  const endFold = resolvesToFold(body, spans[n - 1]!);

  // A single wrapping element: some clients wrap the whole message in one
  // <div> (Gmail, Outlook do). Top-level trimming sees only that one span and
  // would miss blanks nested beside the fold inside it, so recurse into the
  // wrapper's interior and reassemble. Never into a <pre>.
  if (spans.length === 1) {
    const only = spans[0]!;
    const onlyTag = parseTag(body, only.start);
    const isWrap =
      onlyTag &&
      !onlyTag.closing &&
      !onlyTag.selfClose &&
      onlyTag.cls !== "pre" &&
      !only.fold &&
      only.start === 0 &&
      only.end === body.length;
    if (isWrap) {
      const close = "</" + onlyTag.cls + ">";
      if (body.slice(only.end - close.length).toLowerCase() === close) {
        const inner = body.slice(onlyTag.gt, only.end - close.length);
        const trimmed = trimBody(inner);
        if (trimmed === inner) return body;
        return body.slice(0, onlyTag.gt) + trimmed + body.slice(only.end - close.length);
      }
      return body;
    }
  }


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
    let piece = body.slice(s.start, s.end);
    if (s.fold) { parts.push(piece); continue; }
    // The final kept span is the one that meets the trimmed edge, so its own
    // trailing whitespace is peeled as deep as it goes — the <br clear
    // style separator nest Gmail leaves just above a signature block, or a
    // blank run at the very end of a body. The render-time twin of the
    // backend's recursion into the last content node.
    if (i === lastContent) {
      piece = trimTrailingOf(body, s.start, s.end);
    }
    parts.push(piece);
  }
  if (endFold) {
    const fold = spans[n - 1]!;
    parts.push(body.slice(fold.start, fold.end));
  }
  return parts.join("");
}

/**
 * Trim the trailing whitespace-only top-level siblings of a span [start,end),
 * then recurse into the final kept element so blanks nested at any depth are
 * peeled too. Mirrors the backend trimTrailingWhitespace: Gmail renders a
 * message and its signature as adjacent sibling blocks
 * (<div>text…<br clear="all"/></div><div><details class="sig">…</details></div>),
 * and may leave a deeper <br/>&nbsp; run inside the last content block, so the
 * trailing edge is peeled as deep as it goes. Returns the trimmed substring.
 */
function trimTrailingOf(body: string, start: number, end: number): string {
  const inner = body.slice(start, end);
  const subs = topLevelSpans(inner);
  if (subs.length === 0) return "";
  // Drop the trailing run of whitespace-only spans (e.g. a <br clear="all"/>).
  let last = subs.length;
  while (
    last > 0 &&
    !(subs[last - 1]!.fold || subtreeHasContent(inner, subs[last - 1]!.start, subs[last - 1]!.end))
  ) {
    last--;
  }
  if (last === 0) return "";
  const s = subs[last - 1]!;
  // Recurse into the final kept element when it is a block container (a div,
  // table cell, etc. — NOT a <p> or an inline run) so an inner trailing blank
  // run is trimmed too — the <br clear style separator nest Gmail leaves just
  // above the signature block, which sits inside a <div>, not a paragraph. A
  // <p> is treated as terminal: its own trailing empties are the author's
  // text (Gmail u-filler), kept to match the backend's fold-adjacent output.
  const t = parseTag(inner, s.start);
  let sStr = inner.slice(s.start, s.end);
  if (t && CONTAINERS[t.cls] && !t.selfClose) {
    const close = "</" + t.cls + ">";
    if (sStr.slice(sStr.length - close.length).toLowerCase() === close) {
      const openEnd = t.gt;
      const closeStart = s.end - close.length;
      const mid = inner.slice(openEnd, closeStart);
      const trimmedMid = trimBody(mid);
      if (trimmedMid !== mid) {
        sStr = sStr.slice(0, openEnd - s.start) + trimmedMid + close;
      }
    }
  }
  return inner.slice(0, s.start) + sStr; // drop everything after keptEnd
}

// Block containers whose interior tail recursive trim descends into. Leaves
// paragraphs and inline runs alone so their trailing empties (the author's own
// spacer text or Gmail <u></u> filler) are preserved.
const CONTAINERS: Record<string, boolean> = {
  div: true, table: true, tbody: true, thead: true, tfoot: true,
  tr: true, td: true, th: true, section: true, article: true, blockquote: true,
};