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
 * text with the change marked (deleted struck, inserted highlighted) while the
 * quoted message's structure and formatting are kept. The quote's formatting
 * comes from the derived copy — the message the quoter actually pasted — not
 * the bare original, because that is where a pasted message's colour lives
 * (e.g. an answer written in red inside a forwarded thread). Escaping is applied
 * only to the quoter's own added words, so stored plain text cannot inject
 * markup, while the trusted copy HTML passes through verbatim.
 *
 * The copy is walked tag-by-tag rather than stripped to text first, so the
 * unchanged runs keep every tag around them; only words the original lacked get
 * a highlight, in place.
 */
export function editHtml(formattedHtml: string, originalHtml: string, editBody: string): string {
  const srcText = toText(formattedHtml);
  const baseText = toText(originalHtml);
  if (formattedHtml && originalHtml) {
    const cells = tokenize(formattedHtml);
    const srcWords: string[] = [];
    for (const c of cells) if (c.text !== undefined) srcWords.push(...words(c.text));
    if (srcWords.length === words(srcText).length) {
      // Render the copy's own words with its own tags — that is what carries the
      // formatting — and highlight as new the words the original did not have.
      return renderCells(cells, labelsAgainstBase(baseText, srcText));
    }
  }
  // No derived copy to preserve (or the tag-walk misaligned): fall back to the
  // plain base-vs-edit diff that still marks the change.
  return flatHtml(baseText, editBody);
}

/** 'same' where a copy word is also in the original, else 'ins' (new to it). */
function labelsAgainstBase(originalText: string, copyText: string): Array<"same" | "ins"> {
  const labels: Array<"same" | "ins"> = [];
  let ci = 0;
  for (const s of diffBaseToEdit(originalText, copyText)) {
    const sw = words(s.text);
    if (s.kind === "del") continue; // original-only words are absent from the copy
    for (let k = 0; k < sw.length; k++) labels[ci++] = s.kind === "ins" ? "ins" : "same";
  }
  return labels;
}

/** Re-emit the copy's cells verbatim, highlighting runs of new ('ins') words. */
function renderCells(cells: Array<{ tag?: string; text?: string }>, labels: Array<"same" | "ins">): string {
  let wi = 0;
  return cells
    .map((c) => {
      if (c.tag !== undefined) return c.tag;
      // A text cell, as alternating separators (original whitespace) and words.
      const parts = c.text!.split(/(\s+)/);
      const toks: Array<{ sep?: string; word?: string }> = [];
      for (let i = 0; i < parts.length; i++) {
        if (i % 2 === 1) toks.push({ sep: parts[i] });
        else if (parts[i] !== "") toks.push({ word: parts[i] });
      }
      let out = "";
      let i = 0;
      while (i < toks.length) {
        const t = toks[i]!;
        if (t.sep !== undefined) {
          out += t.sep;
          i++;
          continue;
        }
        if (labels[wi] !== "ins") {
          out += t.word;
          wi++;
          i++;
          continue;
        }
        // A run of adjacent inserted words reads as one highlighted phrase;
        // separators between inserted words fold inside, but a trailing
        // separator (before the next kept word) stays out.
        let run = t.word!;
        while (i + 1 < toks.length) {
          const next = toks[i + 1]!;
          if (next.sep !== undefined) {
            // only swallow a separator if what follows it is another insert
            const after = toks[i + 2];
            if (after === undefined || after.word === undefined || labels[wi + 1] !== "ins") break;
            run += next.sep;
            i++;
            continue;
          }
          if (labels[wi + 1] !== "ins") break;
          run += next.word!;
          wi++;
          i++;
        }
        out += `<b class="eins">${run}</b>`;
        wi++;
        i++;
      }
      return out;
    })
    .join("");
}

/**
 * The all-text fallback: diff the original against the quoter's plain text and
 * re-emit escaped, with the change marked.
 */
function flatHtml(originalText: string, editBody: string): string {
  const spans = diffBaseToEdit(originalText, editBody);
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
