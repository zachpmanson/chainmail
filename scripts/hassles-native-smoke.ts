// Hydrated synthetic UI only. CSP denies external connections; no real account/server/Calendar.
import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { build } from "esbuild";

const output = resolve(process.argv[2] ?? "/tmp/chainmail-native-smoke");
await mkdir(output, { recursive: true, mode: 0o700 });
const browser = String.raw`
import React from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import HasslesView from "./src/components/hassles/HasslesView";
import NavHasslesRefresh from "./src/components/navigation/NavHasslesRefresh";
import { nativeOperations } from "./src/lib/hassles/native";
import { hasslesNativeFixture } from "./test/hasslesNativeFixture";
const stage = new URL(location.href).searchParams.get("stage");
const mobile = stage.startsWith("mobile-");
const mode = stage.replace("mobile-", "");
const fixture = hasslesNativeFixture(mobile ? {accountId:"offline",calendarId:"/caldav/v2/"+"long-calendar-identifier".repeat(8)+"@group.calendar.google.com/events/"} : undefined);
let hold, release;
globalThis.fetch = async request => {
  const body = request.method === "POST" ? await request.json() : undefined;
  const result = fixture.handle(request.url, request.method, body);
  if (request.method === "POST" && body?.group?.action === "delete" && hold) await hold;
  return new Response(JSON.stringify(result.data), { status: result.status, headers: {"Content-Type":"application/json"} });
};
let client = new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}});
const root = createRoot(document.getElementById("app"));
const render = () => root.render(<QueryClientProvider client={client}><NavHasslesRefresh/><HasslesView/></QueryClientProvider>);
render();
const wait = async fn => { for(let i=0;i<500;i++){ if(fn()) return; await new Promise(r=>setTimeout(r,10)); } throw Error("Timed out: "+document.getElementById("app").textContent); };
const button = label => [...document.querySelectorAll("button")].find(b=>b.textContent===label || b.getAttribute("aria-label")===label);
const pause = () => new Promise(r=>setTimeout(r,30));
const fill = (el,value) => { Object.getOwnPropertyDescriptor(el.tagName === "TEXTAREA" ? HTMLTextAreaElement.prototype : HTMLSelectElement.prototype,"value").set.call(el,value); el.dispatchEvent(new Event(el.tagName === "TEXTAREA" ? "input":"change",{bubbles:true})); };
const check = (value,message) => {if(!value) throw Error(message);};
const finish = message => { document.getElementById("results").textContent = message; document.getElementById("requests").textContent = JSON.stringify(fixture.requests); };
(async()=>{
  await wait(()=>document.querySelector("textarea") && !document.querySelector("textarea").disabled);
  fill(document.querySelector("textarea"),"Office cycle"); await pause();
  const selects = [...document.querySelectorAll('section[aria-label="Add hassle"] select')];
  fill(selects.find(s=>[...s.options].some(o=>o.value==="hourly")),"hourly"); await pause();
  fill([...document.querySelectorAll("select")].find(s=>[...s.options].some(o=>o.value==="0.5")),"0.5"); await pause();
  button("Add hassle").click(); await wait(()=>button("Delete Office cycle") && !button("Delete Office cycle").disabled);
  check(document.querySelectorAll("li").length===1,"Not one logical card");
  check(document.querySelector("[data-hassle-summary]").textContent.includes("Every 0.5 hours"),"Missing logical cadence");
  check(document.querySelectorAll("[data-hassle-actions] button").length===1,"Legacy actions leaked");
  button("Delete Office cycle").click(); await wait(()=>button("Remove all reminders"));
  check(document.querySelector('[role="dialog"]').textContent.includes("24 components"),"Wrong confirmation count");
  const checkWrap = selector => {
    const element = document.querySelector(selector);
    check(!mobile || innerWidth === 320, "Expected actual 320px viewport, got "+innerWidth);
    check(element.scrollWidth <= element.clientWidth + 1, "New content horizontally overflows");
    check(!mobile || element.getBoundingClientRect().right <= 320, "New content exceeds viewport");
  };
  checkWrap('[role="dialog"]');
  if(mode==="confirmation") return finish("PASS: native one-card and captured confirmation; viewport "+innerWidth+"; no new-content horizontal overflow");
  const partial = mode === "pending" || mode === "verified";
  if(partial) fixture.partial();
  hold = new Promise(r=>release=r);
  button("Remove all reminders").click(); await pause();
  check(document.querySelectorAll("li").length===1,"Optimistic hide before settlement");
  check(document.querySelector("textarea").disabled,"In-flight create enabled");
  release();
  if(!partial){
    await wait(()=>document.querySelectorAll("li").length===0 && !document.querySelector("textarea").disabled);
    const posts=fixture.requests.filter(r=>r.method==="POST");
    check(posts.length===2 && posts.every(r=>r.path==="/v1/hassles/groups"),"Expected one create and one delete, zero legacy");
    check(posts[1].body.group.revision==="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" && posts[1].body.group.fence==="11111111-1111-4111-8111-111111111111:2","Delete witness/fence changed");
    return finish("PASS: 0.5 hourly form submit, one 24-component card, held delete, verified settlement; one create/one delete/zero legacy");
  }
  await wait(()=>document.getElementById("app").textContent.includes("Pending delete: Office cycle"));
  if(mode !== "verified") { root.render(<p>Away</p>); await pause(); client.clear(); client = new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}}); render(); }
  await wait(()=>document.getElementById("app").textContent.includes("Pending delete: Office cycle") && document.querySelector("textarea") && button("Delete Office cycle"));
  check(document.querySelector("textarea").disabled,"Reload allowed new creation");
  check(button("Delete Office cycle").disabled,"Partial card writable");
  const pending = fixture.requests.filter(r=>r.method==="POST").at(-1).body.group.operationId;
  await wait(()=>button("Refresh operation") && !button("Refresh operation").disabled);
  button("Refresh operation").click();
  await wait(()=>fixture.requests.some(r=>r.path==="/v1/hassles/recovery" && r.method==="POST"));
  check(fixture.requests.filter(r=>r.path==="/v1/hassles/groups" && r.method==="POST").length===2,"Group resend during recovery");
  check(fixture.requests.find(r=>r.path==="/v1/hassles/recovery" && r.method==="POST").body.expectedOperationId===pending,"Recovery lost operation identity");
  await pause(); await pause();
  check(document.getElementById("app").textContent.includes("Pending delete: Office cycle"), "Unsettled recovery lost pending metadata");
  check(document.querySelector("textarea").disabled, "Unsettled recovery enabled creation");
  checkWrap('section[aria-label="Reminder operation status"]');
  if(mode === "verified") {
    await wait(()=>button("Refresh operation") && !button("Refresh operation").disabled);
    fixture.verify(); button("Refresh operation").click();
    await wait(()=>!document.getElementById("app").textContent.includes("Pending delete:") && !document.querySelector("textarea").disabled && document.querySelectorAll("li").length===0);
    check(!nativeOperations(client).getSnapshot().request, "Verified recovery retained original request");
    check(fixture.requests.filter(r=>r.path==="/v1/hassles/groups" && r.method==="POST").length===2,"Recovery resent mutator");
    return finish("PASS: naturally unsettled original then explicit verified read-only settlement; no ghost card; controls usable; zero resends");
  }
  finish("PASS: natural partial 409/reload blocked and unsettled expected-ID reconciliation; zero resends; viewport "+innerWidth+"; no banner horizontal overflow");
})().catch(error=>finish("FAIL: "+error.stack));
`;
const assets = "cmd/server/dist/assets";
const css = (
  await Promise.all(
    (await readdir(assets))
      .filter((f) => f.endsWith(".css"))
      .map((f) => readFile(join(assets, f), "utf8")),
  )
).join("\n");
const bundled = await build({
  stdin: { contents: browser, loader: "tsx", resolveDir: process.cwd() },
  bundle: true,
  write: false,
  minify: true,
  define: { "process.env.NODE_ENV": '"production"' },
});
const page = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:"><style>${css.replaceAll("</style", "<\\/style")}</style></head><body><div id="app"></div><pre id="results" style="white-space:pre-wrap;overflow-wrap:anywhere"></pre><pre id="requests" hidden></pre><script>${bundled.outputFiles[0]!.text.replaceAll("</script", "<\\/script")}</script></body></html>`;
await writeFile(join(output, "fixture.html"), page);
const server = createServer((_req, res) => {
  res.writeHead(200, { "Content-Type": "text/html" });
  res.end(page);
});
await new Promise<void>((done) => server.listen(0, "127.0.0.1", done));
const address = server.address();
if (!address || typeof address === "string") throw Error("No loopback listener");
try {
  for (const stage of [
    "confirmation",
    "settled",
    "pending",
    "verified",
    "mobile-confirmation",
    "mobile-pending",
    "mobile-verified",
  ]) {
    const profile = await mkdtemp(join(tmpdir(), "chainmail-native-"));
    const child = spawn("chromium", [
      "--headless",
      "--no-sandbox",
      "--disable-gpu",
      "--disable-dev-shm-usage",
      "--disable-background-networking",
      "--disable-component-update",
      "--disable-sync",
      "--no-first-run",
      "--remote-debugging-port=0",
      "--remote-debugging-address=127.0.0.1",
      `--user-data-dir=${profile}`,
      "about:blank",
    ]);
    let stderr = "";
    child.stderr.on("data", (data) => {
      stderr += String(data);
    });
    let socket: WebSocket | undefined;
    try {
      let port = "";
      for (let i = 0; i < 200; i++) {
        try {
          port = (await readFile(join(profile, "DevToolsActivePort"), "utf8")).split("\n")[0]!;
          break;
        } catch {
          await new Promise((r) => setTimeout(r, 25));
        }
      }
      if (!port) throw Error("No DevTools port");
      const targets = (await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()) as {
        type: string;
        webSocketDebuggerUrl: string;
      }[];
      socket = new WebSocket(targets.find((t) => t.type === "page")!.webSocketDebuggerUrl);
      await new Promise<void>((done, reject) => {
        socket!.onopen = () => done();
        socket!.onerror = reject;
      });
      let id = 0;
      const pending = new Map<
        number,
        { resolve: (value: Record<string, unknown>) => void; reject: (error: Error) => void }
      >();
      socket.onmessage = (event) => {
        const message = JSON.parse(String(event.data));
        if (!message.id) return;
        const call = pending.get(message.id);
        if (!call) return;
        pending.delete(message.id);
        if (message.error) call.reject(Error(JSON.stringify(message.error)));
        else call.resolve(message.result);
      };
      const send = (method: string, params: Record<string, unknown> = {}) =>
        new Promise<Record<string, unknown>>((resolve, reject) => {
          const callId = ++id;
          pending.set(callId, { resolve, reject });
          socket!.send(JSON.stringify({ id: callId, method, params }));
        });
      const evaluate = async (expression: string) => {
        const response = await send("Runtime.evaluate", { expression, returnByValue: true });
        return (response.result as { value: string }).value;
      };
      await send("Page.enable");
      await send("Emulation.setDeviceMetricsOverride", {
        width: stage.startsWith("mobile-") ? 320 : 1100,
        height: 1000,
        deviceScaleFactor: 1,
        mobile: stage.startsWith("mobile-"),
      });
      await send("Page.navigate", { url: `http://127.0.0.1:${address.port}/?stage=${stage}` });
      let result = "";
      for (let i = 0; i < 500; i++) {
        result = await evaluate('document.getElementById("results")?.textContent || ""');
        if (result) break;
        await new Promise((r) => setTimeout(r, 25));
      }
      const html = await evaluate("document.documentElement.outerHTML");
      await writeFile(join(output, `${stage}.html`), html);
      await writeFile(
        join(output, `${stage}-requests.json`),
        await evaluate('document.getElementById("requests").textContent'),
      );
      const screenshot = await send("Page.captureScreenshot", { format: "png" });
      await writeFile(
        join(output, `${stage}.png`),
        Buffer.from(screenshot.data as string, "base64"),
      );
      if (!result.startsWith("PASS:")) throw Error(result || "Browser did not complete");
      console.log(`${stage}: ${result}`);
    } finally {
      socket?.close();
      child.kill("SIGTERM");
      await new Promise<void>((done) => child.on("close", () => done()));
      await writeFile(join(output, `${stage}-chromium.log`), stderr);
      await rm(profile, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 });
    }
  }
} finally {
  server.close();
}
