// Offline geometry regression: actual React markup and production CSS, no server or accounts.
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import HasslesView from "../src/components/hassles/HasslesView";
import { $api } from "../src/lib/api/api";
import { type HassleTask } from "../src/lib/hassles/forms";

const location = { accountId: "offline", calendarId: "/offline-reminders/" };
const task: HassleTask = {
  id: "task:offline-layout",
  revision: "fixture",
  title: "Short reminder",
  reminder: "Offline layout fixture",
  resources: 1,
  writable: true,
  restriction: "",
  instance: null,
  instanceRestriction: "",
  schedule: {
    title: "Short reminder",
    start: "2099-11-06T13:00",
    timeZone: "Australia/Melbourne",
    cadence: "hourly",
    interval: 3,
    until: "",
    workHours: true,
  },
  timing: { state: "upcoming", next: "2099-11-06T02:00:00Z", last: null, note: "" },
};

function markup(tasks: HassleTask[]): string {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData($api.queryOptions("get", "/v1/hassles/location", {}).queryKey, {
    enabled: true,
    location,
  });
  client.setQueryData(
    $api.queryOptions("get", "/v1/hassles", { params: { query: location } }).queryKey,
    { tasks },
  );
  try {
    return renderToStaticMarkup(
      createElement(QueryClientProvider, { client }, createElement(HasslesView)),
    );
  } finally {
    client.clear();
  }
}

// This function runs in Chromium, with no bundled application JS or network calls.
const measureLayouts = String.raw`function () {
  const measurements = [];
  const rect = (element) => {
    const r = element.getBoundingClientRect();
    return { x: r.x, y: r.y, width: r.width, right: r.right, bottom: r.bottom };
  };
  const close = (a, b) => Math.abs(a - b) < 1;
  try {
    for (const root of [14, 16, 20]) {
      document.documentElement.style.fontSize = root + 'px';
      const threshold = 900 + 45 * root; // Content fit plus two 1rem main paddings.
      const widths = [320, 375, 480, 640, 960, threshold - 1, threshold, threshold + 1, 2200];
      for (const variant of ["empty", "short"]) {
        const host = document.querySelector('[data-layout="' + variant + '"]');
        for (const width of widths) {
          host.style.width = width + 'px';
          const main = host.querySelector("main");
          const form = host.querySelector('section[aria-label="Add hassle"]');
          const list = host.querySelector('section[aria-label="Reminders"]');
          const m = rect(main),
            f = rect(form),
            l = rect(list);
          const split = width >= threshold;
          const result = { root, variant, width, split, main: m, form: f, list: l };
          const fail = (message) => {
            throw new Error(message + ': ' + JSON.stringify(result));
          };
          if (!close(m.width, Math.min(width, threshold))) fail("Incorrect main width cap");
          if (!close(l.width, Math.min(40 * root, m.width - 2 * root)))
            fail("List must fill available width up to 40rem");
          if (!close(l.x + l.width / 2, m.x + m.width / 2))
            fail("List must remain independently centred");
          if (!close(f.width, split ? 450 : l.width))
            fail("Form must fill the stacked list width or the 450px sidebar");
          if (split ? !close(f.right + 1.5 * root, l.x) : l.y < f.bottom + 1.5 * root - 1)
            fail("Form must split only when all three tracks fit, otherwise stack above");
          if (main.scrollWidth > main.clientWidth + 1) fail("Main has horizontal overflow");
          for (const control of form.querySelectorAll("input,textarea,select,button")) {
            const c = rect(control);
            if (c.x < f.x - 1 || c.right > f.right + 1) fail("Form control overflow");
          }
          if (list.querySelectorAll("h3").length !== (variant === "short" ? 1 : 0))
            fail("Fixture did not render the intended list contents");
          measurements.push(result);
        }
      }
    }
    return { measurements };
  } catch (error) {
    return { error: String(error), measurements };
  }
}`;

const assets = "cmd/server/dist/assets";
const cssFiles = (await readdir(assets)).filter((name) => name.endsWith(".css"));
if (!cssFiles.length) throw new Error("Run npm run build before the layout smoke test.");
const css = (await Promise.all(cssFiles.map((name) => readFile(join(assets, name), "utf8")))).join(
  "\n",
);
const html = `<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:">
<style>${css.replaceAll("</style", "<\\/style")}</style></head><body>
<div data-layout="empty">${markup([])}</div><div data-layout="short">${markup([task])}</div>
<pre id="layout-results"></pre><script>document.getElementById('layout-results').textContent = encodeURIComponent(JSON.stringify((${measureLayouts})()));</script>
</body></html>`;
const profile = await mkdtemp(join(tmpdir(), "chainmail-layout-"));
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
  const encoded = output.match(/<pre id="layout-results">([^<]+)<\/pre>/)?.[1];
  if (!encoded) throw new Error("Chromium did not return geometry results.");
  const result = JSON.parse(decodeURIComponent(encoded)) as {
    error?: string;
    measurements: object[];
  };
  if (result.error) throw new Error(result.error);
  if (result.measurements.length !== 54) throw new Error("Incomplete geometry matrix.");
  console.log(JSON.stringify(result, null, 2));
  console.log("PASS: 54 offline Chromium layouts (empty/short, roots 14/16/20, fit boundaries).");
} finally {
  await rm(profile, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 });
}
