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
import { buildSync } from "esbuild";
import { dirname, resolve } from "node:path";
import { renderToStaticMarkup } from "react-dom/server";
import Ajv from "ajv";
import { Timeline } from "../src/components/Timeline";
import { normalise } from "../src/lib/normalise";
import { diff, extractSpec, type Mark } from "../src/lib/diff";

const args = process.argv.slice(2);
const flag = (name: string) => {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : undefined;
};
const outPath = flag("-o") ?? flag("--out");
const sincePath = flag("--since");
const consumed = new Set([outPath, sincePath, "-o", "--out", "--since"]);
const specPath = args.find((a) => !a.startsWith("-") && !consumed.has(a));
if (!specPath || !outPath) {
  console.error("usage: render <spec.json> -o <out.html> [--since <previous.html>]");
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

// --since: recover the previous run's spec from the page it produced, and mark
// what this pass added. Diffing is a spec-level operation; the renderer only
// honours the marks.
let marks: Map<string, Mark> | undefined;
let prevLabel: string | undefined;
if (sincePath) {
  try {
    const prev = extractSpec(readFileSync(sincePath, "utf8"));
    marks = diff(prev, spec);
    prevLabel = prev.runLabel ?? sincePath.split("/").pop();
  } catch (e) {
    // a missing baseline is a usage problem, not a crash
    console.error(`--since ${sincePath}: ${e instanceof Error ? e.message : e}`);
    process.exit(1);
  }
}

const css = readFileSync(resolve(root, "src/styles.css"), "utf8");

// Bundle the same behaviour module the dev app uses, so interactivity has one
// implementation rather than a copy that drifts.
const behaviour = buildSync({
  entryPoints: [resolve(root, "src/client/behaviour.ts")],
  bundle: true,
  format: "iife",
  target: "es2020",
  minify: true,
  write: false,
  footer: { js: "chainmail.attach(document);document.body.classList.add('hasmap');" },
  globalName: "chainmail",
}).outputFiles[0]!.text;
const body = renderToStaticMarkup(<Timeline spec={spec} marks={marks} prevLabel={prevLabel} />);
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
<script>${behaviour}</script>
</body></html>
`;

mkdirSync(dirname(resolve(outPath)), { recursive: true });
writeFileSync(outPath, page);
const notes = spec.messages.filter((m) => m.kind === "note").length;
const changed = marks
  ? `, ${[...marks.values()].filter((m) => m === "new").length} new, ` +
    `${[...marks.values()].filter((m) => m === "revised").length} revised vs ${sincePath}`
  : "";
console.log(
  `wrote ${outPath} — ${spec.messages.length - notes} messages, ${notes} notices, ` +
    `${Math.round(page.length / 1024)} KB${changed}`,
);
