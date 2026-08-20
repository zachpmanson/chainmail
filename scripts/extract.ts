/**
 * chainmail extract — print the spec embedded in a rendered page.
 *
 *   chainmail extract page.html > spec.json
 *
 * Rendered HTML is lossy and messy to reparse; the embedded JSON is the exact
 * input, so reloading a previous run is a clean round-trip rather than a scrape.
 */
import { readFileSync } from "node:fs";
import { extractSpec } from "../src/lib/diff";

const path = process.argv[2];
if (!path) {
  console.error("usage: chainmail extract <page.html>");
  process.exit(2);
}

try {
  const spec = extractSpec(readFileSync(path, "utf8"));
  process.stdout.write(JSON.stringify(spec, null, 2) + "\n");
} catch (e) {
  console.error(`${path}: ${e instanceof Error ? e.message : e}`);
  process.exit(1);
}
