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
 * text with the change marked (deleted struck, inserted highlighted) and the
 * original quote's structure and formatting kept. Escaping is applied to the
 * quoter's own words so their stored plain text cannot inject markup, while the
 * base's trusted HTML is passed through verbatim — that is what carries the
 * formatting (paragraphs, bullets, a coloured phrase, an editorial gloss) that
 * a plain-text diff would flatten away.
 *
 * The base is walked tag-by-tag rather than stripped to text first, so the
 * unchanged runs keep every tag around them; only the words the quoter dropped
 * get struck and only the words they added get a highlight, in place.
 */
export function editHtml(baseBodyHtml: string, editBody: string): string {
  const cells = tokenize(baseBodyHtml ?? "");
  const baseWords: string[] = [];
  for (const c of cells) if (c.text !== undefined) baseWords.push(...words(c.text));
  const b = words(toText(baseBodyHtml));
  if (baseWords.length !== b.length) {
    // The tag-walk and the diff disagree on what the words are — a defensive
    // fallback that still marks the change, without risking a misaligned pass.
    return flatHtml(toText(baseBodyHtml), editBody);
  }

  const spans = diffBaseToEdit(toText(baseBodyHtml), editBody);
  // labels[i] tells whether base word i survived (“same”) or was dropped (“del”);
  // inserts[i] holds the quoter’s added words to slide in before base word i.
  const labels: Array<"same" | "del"> = [];
  const inserts: string[][] = [];
  let gi = 0;
  for (const s of spans) {
    const sw = words(s.text);
    if (s.kind === "del") {
      for (let k = 0; k < sw.length; k++) labels[gi++] = "del";
    } else if (s.kind === "ins") {
      (inserts[gi] ??= []).push(...sw);
    } else {
      labels[gi] = "same";
      gi++;
    }
  }

  // Walk the base again, assigning each word to its label. A fresh cursor (not
  // the `gi` above, which ended at the base's word count) so the first word
  // reads label[0].
  let wi = 0;
  let out = cells
    .map((c) => {
      if (c.tag !== undefined) return c.tag;
      // A text cell: its words are consumed in order; separators (original
      // whitespace) pass through so the line keeps its original spacing.
      let cell = "";
      const parts = c.text!.split(/(\s+)/);
      for (let i = 0; i < parts.length; i++) {
        const word = parts[i]!;
        if (i % 2 === 1) {
          cell += word; // a separator
          continue;
        }
        if (word === "") continue;
        for (const ins of inserts[wi] ?? []) cell += `<b class="eins">${escapeHtml(ins)}</b> `;
        cell += labels[wi] === "del" ? `<del class="edel">${word}</del>` : word;
        wi++;
      }
      return cell;
    })
    .join("");
  // Words the quoter added at the very end of the quote (after the last base
  // word) have no base cell to sit before, so append them here.
  for (const ins of inserts[wi] ?? []) out += ` <b class="eins">${escapeHtml(ins)}</b>`;
  return out;
}

/**
 * The old all-text rendering, kept as the alignment fallback: strip the base to
 * its words, diff, and re-emit as escaped text with the change marked.
 */
function flatHtml(baseBodyHtml: string, editBody: string): string {
  const spans = diffBaseToEdit(toText(baseBodyHtml), editBody);
  let out = "";
  for (let i = 0; i < spans.length; i++) {
    const s = spans[i]!;
    if (i > 0) out += " ";
    const inner = escapeHtml(s.text).split("\n").join("<br>");
    if (s.kind === "del") out += `<del class="edel">${inner}</del>`;
    else if (s.kind === "ins") out += `<b class="eins">${inner}</b>`;
    else out += inner;
  }
  return out || escapeHtml(editBody ?? "").split("\n").join("<br>");
}

/** A body split into its tags and the raw text between them, in document order. */
function tokenize(html: string): Array<{ tag?: string; text?: string }> {
  const out: Array<{ tag?: string; text?: string }> = [];
  const re = /<[^>]+>/g;
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(html)) !== null) {
    const pre = html.slice(last, m.index);
    if (pre) out.push({ text: pre });
    out.push({ tag: m[0] });
    last = m.index + m[0].length;
  }
  const rest = html.slice(last);
  if (rest) out.push({ text: rest });
  return out;
}

const escapeHtml = (t: string): string =>
  t.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
