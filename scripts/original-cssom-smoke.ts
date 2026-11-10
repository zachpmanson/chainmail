// Synthetic two-origin CSS only: never loads the mailbox or sender resources.
import assert from "node:assert/strict";
import { execFile, execFileSync } from "node:child_process";
import { createServer, type Server } from "node:http";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import type { AddressInfo } from "node:net";
import { promisify } from "node:util";
import { build } from "esbuild";

const baseline = process.argv.find((arg) => arg.startsWith("--baseline="))?.slice(11);
if (baseline !== undefined && !/^[a-f0-9]{7,40}$/.test(baseline))
  throw new Error("--baseline requires a Git commit hash");
const entry = resolve("src/lib/message/original.ts");
const bundle = await build({
  entryPoints: [entry],
  bundle: true,
  write: false,
  format: "iife",
  globalName: "Original",
  plugins: baseline
    ? [
        {
          name: "committed-baseline",
          setup(build) {
            build.onLoad({ filter: /\/src\/lib\/message\/original\.ts$/ }, () => ({
              contents: execFileSync("git", ["show", `${baseline}:src/lib/message/original.ts`], {
                encoding: "utf8",
              }),
              loader: "ts",
              resolveDir: dirname(entry),
            }));
          },
        },
      ]
    : [],
});
const bundled = bundle.outputFiles[0];
assert(bundled, "Missing browser bundle");
const requests: string[] = [];
const css = createServer((req, res) => {
  requests.push(req.url ?? "");
  assert(!req.headers.authorization && !req.headers.cookie, "Unexpected fixture credentials");
  res.writeHead(200, { "Content-Type": "text/css" });
  res.end(".external { color: rgb(11, 22, 33); }");
});
const listen = (server: Server) =>
  new Promise<number>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolve((server.address() as AddressInfo).port));
  });
const close = (server: Server) =>
  new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
const profile = await mkdtemp(join(tmpdir(), "original-cssom-"));
const page = createServer();
try {
  const cssPort = await listen(css);
  const foreign = `http://127.0.0.1:${cssPort}/foreign.css`;
  const test = String.raw`(async () => {
    const output = document.getElementById("results");
    try {
      const host = document.getElementById("host"), root = host.attachShadow({mode:"open"});
      root.innerHTML = '<style>.kept { font-weight: 700; } @media (prefers-color-scheme: dark) { .kept { color: white; } }</style><link rel="stylesheet" href="${foreign}"><style>@supports (display: grid) { @media (prefers-color-scheme: light) { .tail { color: red; } } .tail { font-style: italic; } }</style><p class="external kept tail">Reader remains usable</p>';
      const link = root.querySelector("link");
      await new Promise((resolve,reject) => { link.onload=resolve; link.onerror=()=>reject(Error("Stylesheet load failed")); setTimeout(()=>reject(Error("Stylesheet load timeout")),2000); });
      const opaque = [...root.styleSheets].find(sheet=>sheet.href==="${foreign}");
      if (!opaque) throw Error("Missing foreign stylesheet");
      let deniedBefore=false; try { void opaque.cssRules; } catch(error) { deniedBefore=error.name==="SecurityError"; }
      if (!deniedBefore) throw Error("Fixture did not establish native CSSOM origin denial");
      Original.dropSchemeVariants(root);
      let deniedAfter=false; try { void opaque.cssRules; } catch(error) { deniedAfter=error.name==="SecurityError"; }
      const readable=[...root.styleSheets].filter(sheet=>sheet!==opaque);
      const text=readable.map(sheet=>[...sheet.cssRules].map(rule=>rule.cssText).join(" ")).join(" ");
      const rendered=getComputedStyle(root.querySelector("p"));
      if (!deniedAfter || !link.isConnected || opaque.disabled) throw Error("Foreign stylesheet/resource boundary changed");
      if (/prefers-color-scheme/.test(text)) throw Error("Readable nested scheme rules survived");
      if (rendered.color!=="rgb(11, 22, 33)" || rendered.fontWeight!=="700" || rendered.fontStyle!=="italic") throw Error("Sender/readable styling lost");
      output.textContent=encodeURIComponent(JSON.stringify({pass:true,nativeOriginDeniedBefore:true,nativeOriginDeniedAfter:true,foreignStylingPreserved:true,readableNestedFiltering:true,text:root.querySelector("p").textContent}));
    } catch(error) { output.textContent=encodeURIComponent(JSON.stringify({error:String(error)})); }
  })();`;
  page.on("request", (_req, res) => {
    res.writeHead(200, {
      "Content-Type": "text/html",
      "Content-Security-Policy": `default-src 'none'; style-src 'unsafe-inline' http://127.0.0.1:${cssPort}; script-src 'unsafe-inline'`,
    });
    res.end(
      `<!doctype html><div id="host"></div><pre id="results"></pre><script>${bundled.text.replaceAll("</script", "<\\/script")}</script><script>${test}</script>`,
    );
  });
  const pagePort = await listen(page);
  const { stdout } = await promisify(execFile)(
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
      "--virtual-time-budget=3000",
      "--dump-dom",
      `http://127.0.0.1:${pagePort}/`,
    ],
    { encoding: "utf8", timeout: 15000, maxBuffer: 4 << 20 },
  );
  const encoded = stdout.match(/<pre id="results">([^<]+)<\/pre>/)?.[1];
  assert(encoded, "Missing browser result");
  const result = JSON.parse(decodeURIComponent(encoded)) as { pass?: boolean; error?: string };
  assert(!result.error, result.error ?? "Browser check failed");
  assert(result.pass);
  assert.deepEqual(requests, ["/foreign.css"]);
  console.log(JSON.stringify({ ...result, baseline: baseline ?? null, requests: requests.length }));
} finally {
  await Promise.all([close(css), close(page)]);
  await rm(profile, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 });
}
