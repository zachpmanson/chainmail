// Actual composer footers captured by draftCardSafety's synthetic DOM fixture, plus built CSS.
// No service, accounts or mail requests; Chromium loads a CSP-restricted data URL.
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

const fixtures = process.env.COMPOSER_LAYOUT_DIR;
if (!fixtures)
  throw new Error(
    "Set COMPOSER_LAYOUT_DIR and run draftCardSafety.test.ts -t 'keeps live body metadata' first.",
  );
const footers = await Promise.all(
  ["reply", "compose"].map(async (kind) => {
    const html = await readFile(join(fixtures, `${kind}-footer.html`), "utf8");
    return `<section data-kind="${kind}">${html}</section>`;
  }),
);
const assets = "cmd/server/dist/assets";
const cssFiles = (await readdir(assets)).filter((name) => name.endsWith(".css"));
if (!cssFiles.length) throw new Error("Run npm run build first.");
const css = (await Promise.all(cssFiles.map((name) => readFile(join(assets, name), "utf8")))).join(
  "\n",
);
const measure = String.raw`function () {
  const results = [];
  const rect = (node) => {
    const r = node.getBoundingClientRect();
    return {x:r.x,y:r.y,right:r.right,bottom:r.bottom,width:r.width,height:r.height};
  };
  const close = (a,b) => Math.abs(a-b) < 1;
  const before = (a,b) => a.bottom <= b.y + 1 || (close(a.y,b.y) && a.right <= b.x + 1);
  try {
    for (const theme of ["light", "dark"]) for (const root of [14,16,20]) for (const width of [280,320,375,640,960]) {
      document.documentElement.dataset.theme = theme;
      document.documentElement.style.fontSize = root + "px";
      document.getElementById("fixture").style.width = width + "px";
      for (const section of document.querySelectorAll("section[data-kind]")) {
        const footer = section.querySelector("footer");
        const count = footer.querySelector("[data-word-count]");
        const labels = [...footer.querySelectorAll("label")];
        const actions = [...footer.querySelectorAll("button")];
        const outer = rect(footer), wordCount = rect(count);
        const controls = [...labels, count].map(rect);
        const buttons = actions.map(rect);
        const result = {kind:section.dataset.kind,theme,root,width,outer,wordCount,controls,buttons};
        const fail = (message) => {throw new Error(message + ": " + JSON.stringify(result));};
        if (section.dataset.kind === "reply") {
          if (labels.length !== 2 || labels.map(n=>n.textContent).join("|") !== "reply all|send html") fail("Missing checkbox order");
          if (count.parentElement !== labels[0].parentElement || count.previousElementSibling !== labels[1]) fail("Count must follow both checkboxes in the left group");
          if (!close(rect(count.parentElement).x,outer.x) || !close(controls[0].x,outer.x)) fail("Checkbox group must stay left aligned");
          if (!before(controls[0],controls[1]) || !before(controls[1],wordCount)) fail("Visual order must be checkboxes then count when wrapping");
        } else if (!close(wordCount.x,outer.x)) fail("New-message word count must stay left aligned");
        if (footer.scrollWidth > footer.clientWidth + 1) fail("Footer has horizontal overflow");
        for (const r of [...controls,...buttons]) {
          if (r.x < outer.x - 1 || r.right > outer.right + 1 || r.width <= 0 || r.height <= 0) fail("Control clipped or missing");
        }
        for (const control of controls) for (const button of buttons) {
          if (control.x < button.right - 1 && control.right > button.x + 1 && control.y < button.bottom - 1 && control.bottom > button.y + 1) fail("Controls overlap actions");
        }
        if (width >= 640 && buttons.length && !close(buttons.at(-1).right,outer.right)) fail("Desktop actions must stay right aligned");
        results.push(result);
      }
    }
    return {results};
  } catch (error) { return {error:String(error),results}; }
}`;
const html = `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'"><style>${css.replaceAll("</style", "<\\/style")}</style></head><body><div id="fixture">${footers.join("")}</div><pre id="results"></pre><script>document.getElementById("results").textContent=encodeURIComponent(JSON.stringify((${measure})()));</script></body></html>`;
const profile = await mkdtemp(join(tmpdir(), "composer-footer-layout-"));
try {
  const output = execFileSync(
    "chromium",
    [
      "--headless",
      "--no-sandbox",
      "--disable-gpu",
      "--disable-dev-shm-usage",
      "--disable-background-networking",
      "--disable-component-update",
      "--disable-sync",
      "--no-first-run",
      `--user-data-dir=${profile}`,
      "--window-size=1600,1200",
      "--dump-dom",
      `data:text/html;base64,${Buffer.from(html).toString("base64")}`,
    ],
    { encoding: "utf8", timeout: 30000, maxBuffer: 16 << 20, stdio: ["ignore", "pipe", "pipe"] },
  );
  const encoded = output.match(/<pre id="results">([^<]+)<\/pre>/)?.[1];
  if (!encoded) throw new Error("Missing Chromium geometry results");
  const result = JSON.parse(decodeURIComponent(encoded)) as { error?: string; results: object[] };
  if (result.error) throw new Error(result.error);
  if (result.results.length !== 60) throw new Error("Incomplete geometry matrix");
  console.log(JSON.stringify(result, null, 2));
  console.log(
    "PASS: 60 offline composer footer layouts (reply/compose, light/dark, 14/16/20px roots, 280–960px widths).",
  );
} finally {
  await rm(profile, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 });
}
