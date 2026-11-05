// Hydrated offline interaction check. Every request is handled in memory, never Calendar.
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { build } from "esbuild";

const browser = String.raw`
import React from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import HasslesView from "./src/components/hassles/HasslesView";
import { hasslesFixture, fixtureLocation } from "./test/hasslesFixture";
const fixture = hasslesFixture();
fixture.handle("/v1/hassles/location", "POST", { location: fixtureLocation, previous: { accountId: "", calendarId: "" }, dedicated: true });
const schedule = { title: "Alpha", start: "2099-01-01T09:00", timeZone: "Australia/Melbourne", cadence: "daily", interval: 1, until: "", workHours: false };
for (const title of ["Alpha", "Beta"]) fixture.handle("/v1/hassles", "POST", { location: fixtureLocation, action: "create", id: crypto.randomUUID(), schedule: { ...schedule, title } });
let releaseEdit, releaseInstance, releaseRemoval;
const edit = new Promise(resolve => releaseEdit = resolve);
const instance = new Promise(resolve => releaseInstance = resolve);
const removal = new Promise(resolve => releaseRemoval = resolve);
let posts = 0;
globalThis.fetch = async (request) => {
  const text = await request.text();
  const body = text ? JSON.parse(text) : undefined;
  if (body?.action === "edit") await edit;
  if (body?.action === "done-instance") await instance;
  const result = fixture.handle(request.url, request.method, body);
  if (request.method === "POST") posts++;
  const payload = JSON.stringify(result.data);
  if (body?.action === "done") await removal;
  return new Response(payload, { status: result.status, headers: { "Content-Type": "application/json" } });
};
const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
createRoot(document.getElementById("app")).render(<QueryClientProvider client={client}><HasslesView /></QueryClientProvider>);
const wait = async (condition) => { for (let i = 0; i < 300; i++) { if (condition()) return; await new Promise(resolve => setTimeout(resolve, 10)); } throw Error("Timed out: " + document.body.textContent); };
const action = label => document.querySelector('button[aria-label="' + label + '"]');
const button = text => [...document.querySelectorAll("button")].find(el => el.textContent === text);
const check = (condition, message) => { if (!condition) throw Error(message); };
const fill = (element, value) => { Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value").set.call(element, value); element.dispatchEvent(new Event("input", { bubbles: true })); };
(async () => {
  await wait(() => action("Edit Alpha"));
  action("Edit Alpha").click();
  await wait(() => document.querySelector('[aria-modal="true"] textarea'));
  fill(document.querySelector('[aria-modal="true"] textarea'), "Alpha edited");
  await new Promise(resolve => setTimeout(resolve, 20));
  button("Save reminder").click();
  await wait(() => action("Edit Alpha edited"));
  check(action("Edit Alpha edited").disabled, "Edit must be visibly optimistic before processing");
  check(!action("Edit Beta").disabled, "Unrelated row blocked");
  check(!document.querySelector('section[aria-label="Add hassle"] textarea').disabled, "Unrelated Add blocked");
  action("This instance only: Beta").click();
  await wait(() => action("Edit Beta").closest("li").textContent.includes("Updating next reminder"));
  check(posts === 0, "Processing gates did not hold writes");
  releaseInstance();
  await wait(() => !action("Edit Beta").disabled);
  check(action("Edit Alpha edited").disabled, "Intermediate GET retired newer overlay");
  releaseEdit();
  await wait(() => !action("Edit Alpha edited").disabled);
  action("All instances: Alpha edited").click();
  await wait(() => button("Remove all reminders"));
  button("Remove all reminders").click();
  await wait(() => !action("Edit Alpha edited"));
  fixture.handle("/__hassles_fixture", "POST", { mode: "inventory-error" });
  releaseRemoval();
  await wait(() => button("Retry refresh"));
  check(document.body.textContent.includes("Saved; awaiting Calendar refresh"), "Acknowledged removal was lost after GET failure");
  check(!action("Edit Alpha edited"), "Failed GET resurrected removed row");
  const before = posts;
  fixture.handle("/__hassles_fixture", "POST", { mode: "normal" });
  button("Retry refresh").click();
  await wait(() => !button("Retry refresh"));
  check(posts === before, "Refresh retried a write");
  check(!action("Edit Alpha edited"), "Removed row resurrected");
  document.getElementById("results").textContent = "PASS: hydrated optimistic edit/completion, independent rows/Add, intermediate read, acknowledged delete/failed GET, GET-only retry";
})().catch(error => { document.getElementById("results").textContent = "FAIL: " + error.stack; });
`;
const assets = "cmd/server/dist/assets";
const css = (
  await Promise.all(
    (await readdir(assets))
      .filter((file) => file.endsWith(".css"))
      .map((file) => readFile(join(assets, file), "utf8")),
  )
).join("\n");
const bundled = await build({
  stdin: { contents: browser, loader: "tsx", resolveDir: process.cwd() },
  bundle: true,
  write: false,
  minify: true,
  define: { "process.env.NODE_ENV": '"production"', "location.origin": '"http://offline.invalid"' },
});
const directory = await mkdtemp(join(tmpdir(), "chainmail-optimistic-"));
try {
  const page = join(directory, "index.html");
  await writeFile(
    page,
    `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:"><style>${css.replaceAll("</style", "<\\/style")}</style></head><body><div id="app"></div><pre id="results"></pre><script>${bundled.outputFiles[0]!.text.replaceAll("</script", "<\\/script")}</script></body></html>`,
  );
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
      `--user-data-dir=${join(directory, "profile")}`,
      "--window-size=375,900",
      "--virtual-time-budget=10000",
      "--dump-dom",
      `file://${page}`,
    ],
    { encoding: "utf8", timeout: 30000, maxBuffer: 16 << 20, stdio: ["ignore", "pipe", "pipe"] },
  );
  const result = output.match(/<pre id="results">([^<]*)<\/pre>/)?.[1];
  if (!result?.startsWith("PASS:"))
    throw new Error(result || "Chromium did not complete the hydrated checks");
  console.log(result);
} finally {
  await rm(directory, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 });
}
