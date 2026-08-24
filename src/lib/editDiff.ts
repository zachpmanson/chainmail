/**
 * editDiff — turn a quoter's modified copy of a quoted message into the spans a
 * bubble can show as a visible edit (issue #42).
 *
 * The backend attaches `edits` to the host message, each carrying the quoter's
 * modified `body` (plain text) plus the spec id of the `base` it was made
 * against. The base's own body reaches the renderer as HTML, so the diff here is
 * plain-text against plain-text: the base's HTML is stripped to its words, then
 * the quoter's version is compared against it.
 *
 * The comparison is a token-level longest common subsequence, which keeps the
 * unchanged runs together and yields short `del`/`ins` hits around the quoter's
 * change — "Amount Due" -> "Invoice Amount" becomes one struck word and one
 * inserted one, not a wholesale rewrite of the line. That keeps the result
 * readable as the sender's edit rather than as a machine-chopped paste.
 */

export interface Span {
  /** how this run of text reads against the base: kept, removed, or inserted */
  kind: "same" | "del" | "ins";
  text: string;
}

// Words/punctuation kept together, so a diff retraces at human-grain.
// Whitespace is deliberately not kept as a token: it is the separating frame
// and would otherwise dominate the LCS with identical runs, causing degenerate
// ties (the "Amount Due" -> "Invoice Amount" example mis-matches when spaces
// compete). The quoter's text is the surface, so span text comes from the edit
// for same/ins and from the base for the struck deletes.
function words(s: string): string[] {
  return s.match(/\S+/g) ?? [];
}

/** The texual content of a message body, for diffing against an edit's plain text. */
export function toText(bodyHtml: string): string {
  return (bodyHtml ?? "")
    .replace(/<[^>]+>/g, " ")
    .replace(ENTITY_RE, (m) => ENTITY[m.slice(1, -1).toLowerCase()] ?? m)
    .trim();
}

const ENTITY_RE = /&[a-z]+;/gi;
/** Named entities the corpus's rendered bodies commonly carry. */
const ENTITY: Record<string, string> = {
  amp: "&", lt: "<", gt: ">", quot: "\"", apos: "'",
  middot: "\u00b7", mdash: "\u2014", ndash: "\u2013",
  hellip: "\u2026", rsquo: "\u2019", lsquo: "\u2018",
  ldquo: "\u201c", rdquo: "\u201d", nbsp: " ",
};

/** Longest common subsequence of two lists of word strings, as index pairs. */
function lcsWords(a: string[], b: string[]): [number[], number[]] {
  const n = a.length, m = b.length;
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i]![j] = a[i]! === b[j]! ? dp[i + 1]![j + 1]! + 1 : Math.max(dp[i + 1]![j]!, dp[i]![j + 1]!);
    }
  }
  const ia: number[] = [], ib: number[] = [];
  let i = 0, j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) { ia.push(i); ib.push(j); i++; j++; }
    else if (dp[i + 1]![j]! >= dp[i]![j + 1]!) i++;
    else j++;
  }
  return [ia, ib];
}

const push = (spans: Span[], kind: Span["kind"], text: string) => {
  if (!text) return;
  spans.push({ kind, text });
};

/**
 * Diff a quoter's modified text against the base's plain text, producing spans
 * in the quoter's order. When the two are effectively identical (or either side
 * is empty / unplaceable) the whole thing is returned unchanged as `same`, so a
 * caller still has a faithful quote to render even where no diff is needed.
 */
export function diffBaseToEdit(baseText: string, editText: string): Span[] {
  if (!baseText || !editText) return [{ kind: "same", text: editText }];
  const a = words(baseText), b = words(editText);
  if (a.length === 0 || b.length === 0) return [{ kind: "same", text: editText }];
  const [ia, ib] = lcsWords(a, b);

  const spans: Span[] = [];
  let ai = 0, ei = 0;
  for (let k = 0; k < ia.length; k++) {
    // base words the quoter dropped, struck in place
    push(spans, "del", a.slice(ai, ia[k]!).join(" "));
    // edit words the base never had, inserted
    push(spans, "ins", b.slice(ei, ib[k]!).join(" "));
    // the shared word as the quoter wrote it
    push(spans, "same", b[ib[k]!]!);
    ai = ia[k]! + 1;
    ei = ib[k]! + 1;
  }
  push(spans, "del", a.slice(ai).join(" "));
  push(spans, "ins", b.slice(ei).join(" "));

  if (spans.length === 0) return [{ kind: "same", text: editText }];
  return spans;
}

/**
 * Render a quoter's edit to markup for the host bubble: the quoter's modified
 * text with the change marked (deleted struck, inserted highlighted). Escaping
 * is applied per run so the stored plain text cannot inject markup.
 */
export function editHtml(baseBodyHtml: string, editBody: string): string {
  const spans = diffBaseToEdit(toText(baseBodyHtml), editBody);
  let out = "";
  for (let i = 0; i < spans.length; i++) {
    const s = spans[i]!;
    if (i > 0) out += " ";
    const inner = escapeHtml(s.text).split("\n").join("<br>");
    if (s.kind === "del") out += `<s class="edel">${inner}</s>`;
    else if (s.kind === "ins") out += `<b class="eins">${inner}</b>`;
    else out += inner;
  }
  return out || escapeHtml(editBody ?? "").split("\n").join("<br>");
}

const escapeHtml = (t: string): string =>
  t.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");