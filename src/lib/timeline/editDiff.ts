/** Diffs a quoter's edited copy of a quoted message against its base (issue #42). */

export interface Span {
  /** how this run of text reads against the base: kept, removed, or inserted */
  kind: "same" | "del" | "ins";
  text: string;
}

// Whitespace isn't a token: identical space runs would dominate the LCS and cause
// degenerate ties.
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
  amp: "&",
  lt: "<",
  gt: ">",
  quot: '"',
  apos: "'",
  middot: "\u00b7",
  mdash: "\u2014",
  ndash: "\u2013",
  hellip: "\u2026",
  rsquo: "\u2019",
  lsquo: "\u2018",
  ldquo: "\u201c",
  rdquo: "\u201d",
  nbsp: " ",
};

/** Longest common subsequence of two lists of word strings, as index pairs. */
function lcsWords(a: string[], b: string[]): [number[], number[]] {
  const n = a.length,
    m = b.length;
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i]![j] =
        a[i]! === b[j]! ? dp[i + 1]![j + 1]! + 1 : Math.max(dp[i + 1]![j]!, dp[i]![j + 1]!);
    }
  }
  const ia: number[] = [],
    ib: number[] = [];
  let i = 0,
    j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      ia.push(i);
      ib.push(j);
      i++;
      j++;
    } else if (dp[i + 1]![j]! >= dp[i]![j + 1]!) i++;
    else j++;
  }
  return [ia, ib];
}

const push = (spans: Span[], kind: Span["kind"], text: string) => {
  if (!text) return;
  spans.push({ kind, text });
};

export function diffBaseToEdit(baseText: string, editText: string): Span[] {
  if (!baseText || !editText) return [{ kind: "same", text: editText }];
  const a = words(baseText),
    b = words(editText);
  if (a.length === 0 || b.length === 0) return [{ kind: "same", text: editText }];
  const [ia, ib] = lcsWords(a, b);

  const spans: Span[] = [];
  let ai = 0,
    ei = 0;
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
 * Formatting comes from the derived copy (where a pasted message's colour lives), not
 * the original. Only the quoter's added words are escaped; the copy HTML is trusted.
 */
export function editHtml(formattedHtml: string, originalHtml: string, editBody: string): string {
  const srcText = toText(formattedHtml);
  const baseText = toText(originalHtml);
  if (formattedHtml && originalHtml) {
    const cells = tokenize(formattedHtml);
    const srcWords: string[] = [];
    for (const c of cells) if (c.text !== undefined) srcWords.push(...words(c.text));
    if (srcWords.length === words(srcText).length) {
      return renderCells(cells, labelsAgainstBase(baseText, srcText));
    }
  }
  // No derived copy, or the tag-walk misaligned.
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
function renderCells(
  cells: Array<{ tag?: string; text?: string }>,
  labels: Array<"same" | "ins">,
): string {
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
        // Separators between inserted words fold into the run; a trailing one stays out.
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
  return (
    out ||
    escapeHtml(editBody ?? "")
      .split("\n")
      .join("<br>")
  );
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
