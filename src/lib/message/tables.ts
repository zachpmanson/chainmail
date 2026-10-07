// A `.txt` is never treated as a table.
const NAMED = /\.(csv|tsv|tab|psv)$/i;
const TYPED = new Set(["text/csv", "application/csv", "text/tab-separated-values", "text/tsv"]);

export const MAX_ROWS = 500;
export const MAX_COLS = 40;

export interface Table {
  /** The file's first row, drawn as the header. */
  header: string[];
  /** The remaining rows, capped to the first `MAX_ROWS`. */
  rows: string[][];
  /** They are padded to the shown width, so every row is the same length. */
  numeric: boolean[];
  /** What the file holds, so the window can say what it is not showing. */
  rowCount: number;
  colCount: number;
}

export function couldBeTable(name: string, type: string): boolean {
  return NAMED.test(name) || TYPED.has(type.split(";")[0]!.trim().toLowerCase());
}

/** RFC 4180: a quote opens a field only at its start, so a stray mid-field quote stays literal. */
export function parseDelimited(body: string, delim: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  let open = false; // a field has started, so a quote would be literal

  const end = () => {
    row.push(field);
    rows.push(row);
    row = [];
    field = "";
    open = false;
  };

  for (let i = 0; i < body.length; i++) {
    const ch = body[i]!;
    if (quoted) {
      if (ch !== '"') {
        field += ch;
        continue;
      }
      if (body[i + 1] === '"') {
        field += '"';
        i++;
        continue;
      }
      quoted = false;
      continue;
    }
    if (ch === '"' && !open) {
      quoted = true;
      open = true;
      continue;
    }
    if (ch === delim) {
      row.push(field);
      field = "";
      open = false;
      continue;
    }
    if (ch === "\n") {
      end();
      continue;
    }
    // A bare CR inside a quoted field is content and handled above.
    if (ch === "\r") continue;
    field += ch;
    open = true;
  }
  if (open || field !== "" || row.length) end();
  return rows;
}

/** Sniffed rather than taken from the extension: a `.csv` may be semicolon- or pipe-delimited. */
export function chooseDelimiter(body: string, name: string, type: string): string | null {
  const tabby =
    /\.(tsv|tab)$/i.test(name) ||
    type.split(";")[0]!.trim().toLowerCase() === "text/tab-separated-values";
  return tabby ? "\t" : sniff(body);
}

const CANDIDATES = [",", "\t", ";", "|"];

const SPLIT_ENOUGH = 0.8;

function sniff(body: string): string | null {
  const sample = body.slice(0, 8192);
  const whole = sample.length === body.length;
  let best: { delim: string; cols: number; score: number } | null = null;
  for (const delim of CANDIDATES) {
    const rows = parseDelimited(sample, delim).filter((r) => r.some((f) => f.trim() !== ""));
    // A partial last row is not evidence about the shape: it was cut mid-line.
    if (!whole && !sample.endsWith("\n")) rows.pop();
    if (!rows.length) continue;
    const split = rows.filter((r) => r.length >= 2).length / rows.length;
    if (split < SPLIT_ENOUGH) continue;
    const cols = Math.max(...rows.map((r) => r.length));
    if (!best || split > best.score || (split === best.score && cols > best.cols))
      best = { delim, cols, score: split };
  }
  return best?.delim ?? null;
}

/** Read a file as a table, or say that it is not one. */
export function readTable(body: string, name: string, type: string): Table | null {
  if (!couldBeTable(name, type)) return null;
  const delim = chooseDelimiter(body, name, type);
  if (!delim) return null;

  const rows = parseDelimited(body, delim).filter((r) => r.some((f) => f.trim() !== ""));
  if (rows.length < 2) return null; // a header and nothing under it is a line of text

  const colCount = Math.max(...rows.map((r) => r.length));
  if (colCount < 2) return null; // one column is a list, and a list is text

  const shown = Math.min(colCount, MAX_COLS);
  const pad = (r: string[]) => {
    const out = r.slice(0, shown);
    while (out.length < shown) out.push("");
    return out;
  };
  const header = pad(rows[0]!);
  const data = rows.slice(1);
  const shownRows = data.slice(0, MAX_ROWS).map(pad);
  return {
    header,
    rows: shownRows,
    numeric: numericColumns(shownRows),
    rowCount: data.length,
    colCount,
  };
}

/** Mostly numeric (tolerating a stray "n/a") and at least two cells. */
function numericColumns(rows: string[][]): boolean[] {
  const NUMERIC = /^[-+(]?\s*[$€£]?\s*\d[\d,\s]*(\.\d+)?\s*%?\)?$/;
  const width = rows[0]?.length ?? 0;
  return Array.from({ length: width }, (_, i) => {
    const cells = rows.map((r) => r[i] ?? "").filter((c) => c.trim() !== "");
    if (cells.length < 2) return false;
    return cells.filter((c) => NUMERIC.test(c.trim())).length / cells.length >= 2 / 3;
  });
}
