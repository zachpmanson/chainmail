/**
 * Reading a delimited file as a table.
 *
 * CSV is a format with a specification that most of the world's exports ignore:
 * fields are quoted, quotes are doubled, a field can contain the delimiter and a
 * newline, and the delimiter itself is a comma in one country's spreadsheet and a
 * semicolon in another's. What lands in the corpus is somebody's real export, so
 * the reader below is the RFC 4180 one rather than a `split(",")` — and the
 * delimiter is read off the file instead of assumed from its name.
 *
 * Deliberately no parsing on the server: a saved spec is a *derivation*, and a page
 * written before this existed carries `view: "text"` for a CSV and no more. The
 * bytes are what decide, and the bytes arrive with the window.
 */

/** The extensions and types that can be a table at all. A `.txt` is prose until
 *  proven otherwise, and prose with commas in it must not become a spreadsheet. */
const NAMED = /\.(csv|tsv|tab|psv)$/i;
const TYPED = new Set([
  "text/csv",
  "application/csv",
  "text/tab-separated-values",
  "text/tsv",
]);

/** What a window will draw before it stops being a table and starts being a
 *  wall: rows to scroll, columns to read across. The file itself is a click away
 *  in the bar under it, which is where a wide or long one belongs anyway. */
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

/** Could this file be a table? Asked of the name and the served type, before a
 *  single byte is parsed. */
export function couldBeTable(name: string, type: string): boolean {
  return NAMED.test(name) || TYPED.has(type.split(";")[0]!.trim().toLowerCase());
}

/**
 * One delimited file as rows of fields.
 *
 * A quote opens a field only at its start, which is what RFC 4180 says and also
 * what makes a malformed one behave: a stray quote mid-field (a measurement in
 * inches, a name like O"Brien) stays a quote rather than swallowing the rest of
 * the file.
 */
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
      if (ch !== '"') { field += ch; continue; }
      if (body[i + 1] === '"') { field += '"'; i++; continue; }
      quoted = false;
      continue;
    }
    if (ch === '"' && !open) { quoted = true; open = true; continue; }
    if (ch === delim) { row.push(field); field = ""; open = false; continue; }
    if (ch === "\n") { end(); continue; }
    // A carriage return is the other half of a CRLF and nothing else here: a
    // bare one inside a quoted field is content, and is only reachable above.
    if (ch === "\r") continue;
    field += ch;
    open = true;
  }
  if (open || field !== "" || row.length) end();
  return rows;
}

/** The delimiter this file actually uses, or null if it does not use one.
 *
 *  Read rather than declared: a `.tsv` is tabbed by definition, but everything
 *  else is tried — a semicolon export named `.csv` is what a European
 *  spreadsheet writes, and a pipe-delimited file is what a mainframe writes.
 *  Each candidate is scored on the share of rows it actually splits in two, so a
 *  file that only mentions a delimiter in passing loses to the one whose whole
 *  shape is built on it. */
export function chooseDelimiter(body: string, name: string, type: string): string | null {
  const tabby =
    /\.(tsv|tab)$/i.test(name) || type.split(";")[0]!.trim().toLowerCase() === "text/tab-separated-values";
  return tabby ? "\t" : sniff(body);
}

const CANDIDATES = [",", "\t", ";", "|"];

/** Below this share of rows, the "table" is a file with the delimiter in its
 *  prose — a comma in a sentence is not a column. */
const SPLIT_ENOUGH = 0.8;

function sniff(body: string): string | null {
  // The first rows decide; a file whose shape changes later is ragged, and ragged
  // is padded rather than refused (see readTable).
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

/** Which columns read as numbers: right-aligned where they are, because a column
 *  of amounts is scanned by its digits, not by its left edge.
 *
 *  A column has to be mostly numeric — a stray "n/a" in a column of figures must
 *  not stop it reading as one, and with three rows one stray is already a third of
 *  it — and at least two cells, because one number proves nothing about a column. */
function numericColumns(rows: string[][]): boolean[] {
  const NUMERIC = /^[-+(]?\s*[$€£]?\s*\d[\d,\s]*(\.\d+)?\s*%?\)?$/;
  const width = rows[0]?.length ?? 0;
  return Array.from({ length: width }, (_, i) => {
    const cells = rows.map((r) => r[i] ?? "").filter((c) => c.trim() !== "");
    if (cells.length < 2) return false;
    return cells.filter((c) => NUMERIC.test(c.trim())).length / cells.length >= 2 / 3;
  });
}
