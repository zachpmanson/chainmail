// Production React/CSS geometry only: no server, account, credentials or Calendar requests.
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import HassleList from "../src/components/hassles/HassleList";
import type { HassleTask } from "../src/lib/hassles/forms";

const task: HassleTask = {
  id: "task:card-row",
  revision: "fixture",
  title:
    "Return the library books and choose something new for the weekend with a full-width title",
  reminder: "Timing unavailable",
  resources: 1,
  writable: true,
  restriction: "",
  instanceRestriction: "",
  instance: { at: "2099-11-06T02:00:00Z", token: "fixture" },
  schedule: {
    title: "Fixture",
    start: "2099-11-06T13:00",
    timeZone: "Australia/Melbourne",
    cadence: "daily",
    interval: 1,
    until: "",
    workHours: true,
  },
  timing: { state: "upcoming", next: "2099-11-06T02:00:00Z", last: null, note: "" },
};
const tasks: HassleTask[] = [
  task,
  { ...task, id: "task:short", title: "Short reminder", schedule: null },
  {
    ...task,
    id: "task:unbroken",
    title: "Unbroken".repeat(30),
    schedule: null,
    writable: false,
    reminder: "Unknown imported timing ".repeat(12),
    restriction: "Protected".repeat(25),
    timing: {
      state: "unknown",
      next: null,
      last: null,
      note: "Incomplete timing remains visible.",
    },
  },
];
tasks.push({ ...task, id: "task:unbroken-work", title: "Unbroken".repeat(30) });
const markup = renderToStaticMarkup(
  createElement(HassleList, {
    tasks,
    busy: false,
    onEdit: () => {},
    onDone: () => {},
    onDoneInstance: () => {},
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
  try {
    for (const theme of ["light", "dark"]) for (const root of [14,16,20]) for (const width of [320,375,480,640,960]) {
      document.documentElement.dataset.theme = theme;
      document.documentElement.style.fontSize = root + "px";
      document.getElementById("fixture").style.width = width + "px";
      const cards = [...document.querySelectorAll("li")];
      if (cards.length !== 4) throw new Error("Missing fixture cards");
      for (const card of cards) {
        const heading = rect(card.querySelector("[data-hassle-heading]"));
        const title = rect(card.querySelector("[data-hassle-title]"));
        const workNode = card.querySelector("[data-hassle-work]");
        const work = workNode ? rect(workNode) : null;
        const details = rect(card.querySelector("[data-hassle-details]"));
        const summary = rect(card.querySelector("[data-hassle-summary]"));
        const actions = rect(card.querySelector("[data-hassle-actions]"));
        const buttons = [...card.querySelectorAll("button")].map(rect);
        const outer = rect(card), style = getComputedStyle(card);
        const left = outer.x + parseFloat(style.borderLeftWidth) + parseFloat(style.paddingLeft);
        const right = outer.right - parseFloat(style.borderRightWidth) - parseFloat(style.paddingRight);
        const result = {theme,root,width,heading,title,work,details,summary,actions,buttons};
        const fail = (message) => {throw new Error(message + ": " + JSON.stringify(result));};
        if (!close(heading.x,left) || !close(heading.right,right) || !close(title.x,left)) fail("Heading must span the card interior");
        if (work) {
          if (!close(work.right,right) || !close(work.y,heading.y) || title.right >= work.x || work.bottom > heading.bottom + 1) fail("Work badge must sit at top right without overlapping the title");
          if (card.querySelector("[data-hassle-summary]").textContent.includes("work")) fail("Work badge must leave the summary");
        } else if (!close(title.right,right)) fail("Title without badge must keep the full width");
        if (details.y < heading.bottom || !close(details.width,heading.width)) fail("Details must be below the heading");
        if (!close(summary.x,left) || summary.width <= 0 || actions.x < summary.right) fail("Summary and actions must share one row");
        if (!close(actions.y + actions.height/2, details.y + details.height/2)) fail("Actions must align with grey summary row");
        if (buttons.length !== 3 || buttons.some((r) => !close(r.y,buttons[0].y))) fail("All three buttons must stay inline");
        if (buttons.some((r,i) => r.x < left || r.right > right + 1 || (i > 0 && r.x < buttons[i-1].right))) fail("Buttons overlap or overflow");
        if (card.scrollWidth > card.clientWidth + 1 || card.querySelector("h3").scrollWidth > card.querySelector("h3").clientWidth + 1) fail("Card or title has horizontal overflow");
        results.push(result);
      }
    }
    return {results};
  } catch (error) { return {error:String(error),results}; }
}`;
const html = `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:"><style>${css.replaceAll("</style", "<\\/style")}</style></head><body><div id="fixture">${markup}</div><pre id="results"></pre><script>document.getElementById("results").textContent=encodeURIComponent(JSON.stringify((${measure})()));</script></body></html>`;
const profile = await mkdtemp(join(tmpdir(), "hassle-card-layout-"));
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
      "--window-size=2200,1200",
      "--dump-dom",
      `data:text/html;base64,${Buffer.from(html).toString("base64")}`,
    ],
    { encoding: "utf8", timeout: 30000, maxBuffer: 16 << 20, stdio: ["ignore", "pipe", "pipe"] },
  );
  const encoded = output.match(/<pre id="results">([^<]+)<\/pre>/)?.[1];
  if (!encoded) throw new Error("Missing Chromium geometry results");
  const result = JSON.parse(decodeURIComponent(encoded)) as { error?: string; results: object[] };
  if (result.error) throw new Error(result.error);
  if (result.results.length !== 120) throw new Error("Incomplete geometry matrix");
  console.log(JSON.stringify(result, null, 2));
  console.log(
    "PASS: 120 offline card layouts (light/dark, 14/16/20px roots, 320–960px, short/long/unbroken titles with and without work badges).",
  );
} finally {
  await rm(profile, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 });
}
