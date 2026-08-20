/**
 * chainmail render — turn a timeline spec into one self-contained HTML file.
 *
 *   npm run render -- fixtures/synthetic.json -o out/page.html
 *
 * Server-renders rather than shipping a client-rendered shell, so the artifact
 * stays static: greppable, printable, and readable with scripting disabled.
 * Interactivity is layered on top, not required to see the content.
 */
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { renderToStaticMarkup } from "react-dom/server";
import Ajv from "ajv";
import { Timeline } from "../src/components/Timeline";
import { normalise } from "../src/lib/normalise";

const args = process.argv.slice(2);
const outIx = args.findIndex((a) => a === "-o" || a === "--out");
const specPath = args.find((a) => !a.startsWith("-") && args.indexOf(a) !== outIx + 1);
const outPath = outIx >= 0 ? args[outIx + 1] : undefined;
if (!specPath || !outPath) {
  console.error("usage: render <spec.json> -o <out.html>");
  process.exit(2);
}

const root = resolve(import.meta.dirname, "..");
const raw = JSON.parse(readFileSync(specPath, "utf8"));
const spec = normalise(raw);

// Validate at the boundary: a spec that violates the contract should fail loudly
// here rather than render a subtly wrong page.
const schema = JSON.parse(readFileSync(resolve(root, "schema/timeline.schema.json"), "utf8"));
const ajv = new Ajv({ allErrors: true, strict: false });
if (!ajv.validate(schema, spec)) {
  console.error("spec does not match schema/timeline.schema.json:");
  for (const e of ajv.errors ?? []) console.error(`  ${e.instancePath || "/"} ${e.message}`);
  process.exit(1);
}

const css = readFileSync(resolve(root, "src/styles.css"), "utf8");
const body = renderToStaticMarkup(<Timeline spec={spec} />);
const theme = spec.theme ?? "light";
const title = (spec.title ?? "Timeline").replace(/^#+/, "");

// The spec travels with the page, so a later pass reloads exact structured input
// instead of scraping rendered HTML.
const embedded = JSON.stringify(spec).replace(/<\//g, "<\\/");

const page = `<!doctype html>
<html lang="en"${theme === "auto" ? "" : ` data-theme="${theme}"`}>
<head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${title}</title>
<style>${css}</style></head>
<body>${body}
<script type="application/json" id="chainmail-spec">${embedded}</script>
</body></html>
`;

mkdirSync(dirname(resolve(outPath)), { recursive: true });
writeFileSync(outPath, page);
const notes = spec.messages.filter((m) => m.kind === "note").length;
console.log(
  `wrote ${outPath} — ${spec.messages.length - notes} messages, ${notes} notices, ` +
    `${Math.round(page.length / 1024)} KB`,
);
