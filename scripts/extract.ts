/** chainmail extract page.html > spec.json — print the spec embedded in a rendered page. */
import { readFileSync } from "node:fs";
import { extractSpec } from "../src/lib/timeline/diff";

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
