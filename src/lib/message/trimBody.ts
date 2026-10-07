/**
 * Render-time twin of the backend's trimEdgeWhitespace (internal/spec/htmlbody.go),
 * for pages baked before that shipped. Splices the serialized string, so it is idempotent.
 */

const VOID = new Set([
  "area",
  "base",
  "br",
  "col",
  "embed",
  "hr",
  "img",
  "input",
  "link",
  "meta",
  "param",
  "source",
  "track",
  "wbr",
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
    if (quote) {
      if (ch === quote) quote = "";
    } else if (ch === '"' || ch === "'") quote = ch;
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

/** An unbalanced tail is kept as one span so no content is ever dropped. */
function topLevelSpans(body: string): { start: number; end: number; fold: boolean }[] {
  const out: { start: number; end: number; fold: boolean }[] = [];
  const push = (start: number, end: number, fold: boolean) => {
    if (end > start) out.push({ start, end, fold });
  };
  let i = 0;
  const len = body.length;
  while (i < len) {
    const lt = body.indexOf("<", i);
    if (lt === -1) {
      push(i, len, false);
      break;
    }
    if (lt > i) {
      push(i, lt, false);
      i = lt;
      continue;
    }
    const t = parseTag(body, lt);
    if (!t) {
      i++;
      continue;
    }
    if (t.closing) {
      i = t.gt;
      continue;
    } // stray close; skip
    if (t.selfClose) {
      push(lt, t.gt, false);
      i = t.gt;
      continue;
    }
    // find matching close for this element name
    let depth = 1;
    let j = t.gt;
    let end = -1;
    while (j < len) {
      const lt2 = body.indexOf("<", j);
      if (lt2 === -1) break;
      const t2 = parseTag(body, lt2);
      if (!t2) {
        j = lt2 + 1;
        continue;
      }
      if (t2.closing && t2.cls === t.cls) {
        depth--;
        if (depth === 0) {
          end = t2.gt;
          break;
        }
      } else if (!t2.closing && !t2.selfClose && t2.cls === t.cls) {
        depth++;
      }
      j = t2.gt;
    }
    if (end === -1) {
      push(lt, len, false);
      break;
    }
    const fold = t.cls === "details" && hasClass(t.classes, "sig");
    push(lt, end, fold);
    i = end;
  }
  return out;
}

function hasClass(classes: string, want: string): boolean {
  return classes.split(/\s+/).includes(want);
}

/** Some clients wrap the fold, e.g. <div><details class="sig">…</details></div>; that counts as the fold. */
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

/** Uses the trim's definition of content, except that HTML comments are ignored. */
export function hasBody(body: string): boolean {
  // Some clients send whole tables inside conditional comments, which browsers never draw.
  const drawn = body
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/<(script|style)\b[\s\S]*?<\/\1\s*>/gi, "");
  return topLevelSpans(drawn).some((s) => s.fold || subtreeHasContent(drawn, s.start, s.end));
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

  const endFold = resolvesToFold(body, spans[n - 1]!);

  // Gmail and Outlook wrap the whole message in one <div>, so recurse into it. Never into a <pre>.
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

  let last = endFold ? n - 2 : n - 1;
  while (last >= first && !content(last)) last--;

  // Only a fold is content: keep just the fold.
  const lastContent = last;
  if (lastContent < first) {
    return endFold ? body.slice(spans[n - 1]!.start, spans[n - 1]!.end) : "";
  }

  if (!endFold && first === 0 && lastContent === n - 1) {
    // Gmail opens the signature wrap with <br clear="all"/>, which can still need peeling.
    const stripped = stripWrapLead(body.slice(spans[n - 1]!.start, spans[n - 1]!.end));
    if (stripped == null) return body;
    return body.slice(0, spans[n - 1]!.start) + stripped;
  }

  // Blanks between content spans are the author's separators and stay.
  const parts: string[] = [];
  for (let i = first; i <= lastContent; i++) {
    const s = spans[i]!;
    let piece = body.slice(s.start, s.end);
    if (s.fold) {
      parts.push(piece);
      continue;
    }
    // Peel the last span's trailing whitespace as deep as it goes, like the backend's recursion.
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
 * Peels leading blanks inside a wrapper that ends in a signature fold (Gmail's
 * <br clear="all"/>). Mirrors the backend's trimLeadingWhitespace. Null if not that shape.
 */
function stripWrapLead(innerOfSpan: string): string | null {
  const t = parseTag(innerOfSpan, 0);
  if (!t || t.closing || t.selfClose || t.cls === "pre") return null;
  const close = "</" + t.cls + ">";
  if (innerOfSpan.slice(innerOfSpan.length - close.length).toLowerCase() !== close) return null;
  const mid = innerOfSpan.slice(t.gt, innerOfSpan.length - close.length);
  const subs = topLevelSpans(mid);
  let first = 0;
  while (
    first < subs.length &&
    !(subs[first]!.fold || subtreeHasContent(mid, subs[first]!.start, subs[first]!.end))
  ) {
    first++;
  }
  if (first === 0) return null;
  if (first === subs.length) return null;
  const abs = { start: subs[first]!.start, end: subs[first]!.end };
  if (!resolvesToFold(mid, abs)) return null;
  return innerOfSpan.slice(0, t.gt) + innerOfSpan.slice(t.gt + subs[first]!.start);
}
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
  // A <p> is terminal: its trailing empties (Gmail's <u></u> filler) are kept to match the backend.
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

const CONTAINERS: Record<string, boolean> = {
  div: true,
  table: true,
  tbody: true,
  thead: true,
  tfoot: true,
  tr: true,
  td: true,
  th: true,
  section: true,
  article: true,
  blockquote: true,
};
