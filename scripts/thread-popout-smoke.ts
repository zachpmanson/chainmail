// Built app, real two-page Chromium/BroadcastChannel; only invented loopback HTTP fixtures.
import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, extname } from "node:path";
import assert from "node:assert/strict";

const profile = await mkdtemp(join(tmpdir(), "chainmail-popout-"));
const writes: string[] = [];
const reads: string[] = [];
const chainScopes: (string | null)[] = [];
let draft: Record<string, unknown> | undefined;
const entry = {
  extId: "parent",
  source: "mail",
  quoted: false,
  ts: "2026-01-01T00:00:00Z",
  subject: "Synthetic popup",
  fromEmail: "sender@example.test",
  permalink: "https://mail.invalid/#all/parent",
  body: "Invented message",
  participants: [],
};
const summary = {
  rootExtId: "root",
  subject: "Synthetic popup",
  entries: 1,
  people: 1,
  attachments: 0,
  unread: 0,
};
const server = createServer(async (req, res) => {
  const url = new URL(req.url!, "http://localhost");
  reads.push(url.pathname);
  const json = (value: unknown) => {
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify(value));
  };
  if (req.method !== "GET") {
    let text = "";
    for await (const chunk of req) text += chunk;
    const body = text ? JSON.parse(text) : {};
    assert(["/v1/drafts", "/v1/drafts/d1"].includes(url.pathname), `Unexpected write ${url}`);
    writes.push(req.method!);
    if (req.method === "DELETE") {
      assert.equal(url.pathname, "/v1/drafts/d1");
      draft = undefined;
      json({ deleted: true });
      return;
    }
    draft = {
      accountId: "A",
      id: "d1",
      messageId: `m${writes.length}`,
      kind: "reply",
      resolution: "resolved",
      rootId: "root",
      replyToEntry: "parent",
      to: "sender@example.test",
      cc: "",
      body: body.body,
      subject: "Re: Synthetic popup",
    };
    json(draft);
    return;
  }
  if (url.pathname === "/auth/status") {
    json({
      accounts: [{ id: "A", signedIn: true, email: "me@example.test", displayName: "Synthetic" }],
    });
    return;
  }
  if (url.pathname === "/v1/settings") {
    json({ defaultFolder: "Inbox" });
    return;
  }
  if (url.pathname === "/v1/people") {
    json({ people: [] });
    return;
  }
  if (url.pathname === "/v1/search") {
    json({
      chains: [summary, { ...summary, rootExtId: "reader", subject: "Synthetic read-only" }],
    });
    return;
  }
  if (url.pathname.startsWith("/v1/chains/")) {
    chainScopes.push(url.searchParams.get("accountId"));
    const reader = url.pathname.endsWith("/reader");
    json({
      entries: [
        reader
          ? {
              ...entry,
              extId: "slack:channel:1",
              source: "slack",
              permalink: "https://chat.invalid/message",
            }
          : entry,
      ],
      summary: reader ? { ...summary, rootExtId: "reader" } : summary,
    });
    return;
  }
  if (url.pathname === "/v1/drafts") {
    json(draft ? [draft] : []);
    return;
  }
  if (url.pathname.endsWith("/save-check")) {
    json({
      status: "supported",
      context: "reply",
      unchangedCompatible: true,
      bodyEditCompatible: true,
    });
    return;
  }
  if (url.pathname === "/v1/drafts/d1") {
    if (!draft) res.statusCode = 404;
    json(draft ?? { error: "Synthetic draft deleted" });
    return;
  }
  if (url.pathname.startsWith("/v1/")) {
    json({});
    return;
  }
  try {
    const path = url.pathname.startsWith("/assets/") ? url.pathname : "/index.html";
    let bytes = await readFile(join("cmd/server/dist", path));
    if (path === "/index.html")
      bytes = Buffer.from(
        bytes
          .toString()
          .replace(
            "<head>",
            `<head><meta http-equiv="Content-Security-Policy" content="default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self'">`,
          ),
      );
    res.setHeader(
      "Content-Type",
      (
        { ".js": "text/javascript", ".css": "text/css", ".html": "text/html" } as Record<
          string,
          string
        >
      )[extname(path)] ?? "application/octet-stream",
    );
    res.end(bytes);
  } catch {
    res.statusCode = 404;
    res.end();
  }
});
await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
const port = (server.address() as { port: number }).port;
const browser = spawn(
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
    "--disable-popup-blocking",
    "--remote-debugging-port=0",
    `--user-data-dir=${profile}`,
    "--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE localhost, EXCLUDE 127.0.0.1",
    "--no-proxy-server",
    "about:blank",
  ],
  { stdio: ["ignore", "ignore", "pipe"] },
);
class CDP {
  acceptDialogs = true;
  dialogs: string[] = [];
  sequence = 0;
  pending = new Map<
    number,
    { resolve: (value: unknown) => void; reject: (error: Error) => void }
  >();
  socket: WebSocket;
  ready: Promise<void>;
  constructor(url: string) {
    this.socket = new WebSocket(url);
    this.ready = new Promise((resolve) => this.socket.addEventListener("open", () => resolve()));
    this.socket.addEventListener("message", ({ data }) => {
      const packet = JSON.parse(String(data));
      if (packet.method === "Page.javascriptDialogOpening") {
        this.dialogs.push(packet.params.type);
        void this.call("Page.handleJavaScriptDialog", { accept: this.acceptDialogs });
      }
      const pending = this.pending.get(packet.id);
      if (pending) {
        this.pending.delete(packet.id);
        if (packet.error) pending.reject(new Error(JSON.stringify(packet.error)));
        else pending.resolve(packet.result);
      }
    });
  }
  async call<T = Record<string, unknown>>(method: string, params = {}): Promise<T> {
    await this.ready;
    const id = ++this.sequence;
    const promise = new Promise<unknown>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`CDP timeout: ${method}`));
      }, 20000);
      this.pending.set(id, {
        resolve: (value) => {
          clearTimeout(timer);
          resolve(value);
        },
        reject: (error) => {
          clearTimeout(timer);
          reject(error);
        },
      });
    });
    this.socket.send(JSON.stringify({ id, method, params }));
    return promise as Promise<T>;
  }
  async evaluate(expression: string) {
    const result = await this.call<{ exceptionDetails?: unknown; result: { value: unknown } }>(
      "Runtime.evaluate",
      {
        expression,
        returnByValue: true,
        awaitPromise: true,
        userGesture: true,
      },
    );
    if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
    return result.result.value;
  }
  async until(expression: string) {
    assert.equal(
      await this.evaluate(
        `new Promise(resolve => { const end=Date.now()+15000; const check=()=>{if (${expression}) resolve(true); else if(Date.now()>end) resolve(false); else setTimeout(check,30)};check()})`,
      ),
      true,
      expression,
    );
  }
}
let source: CDP | undefined;
let popup: CDP | undefined;
try {
  const debug = await new Promise<string>((resolve, reject) => {
    let stderr = "";
    const timer = setTimeout(() => reject(new Error("Chromium did not start")), 15000);
    browser.stderr!.on("data", (chunk) => {
      stderr += chunk;
      const found = stderr.match(/DevTools listening on ws:\/\/127\.0\.0\.1:(\d+)/);
      if (found) {
        clearTimeout(timer);
        resolve(`http://127.0.0.1:${found[1]}`);
      }
    });
    browser.on("error", reject);
  });
  const pages = async (): Promise<{ id: string; url: string; webSocketDebuggerUrl: string }[]> =>
    (await fetch(debug + "/json/list")).json();
  source = new CDP(
    (await pages()).find((page) => page.url === "about:blank")!.webSocketDebuggerUrl,
  );
  const delay = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
  const click = async (page: CDP, label: string) => {
    await page.until(
      `[...document.querySelectorAll('button')].some(n=>n.textContent===${JSON.stringify(label)})`,
    );
    return page.evaluate(
      `[...document.querySelectorAll('button')].find(n=>n.textContent===${JSON.stringify(label)}).click()`,
    );
  };
  const input = async (page: CDP, selector: string, text: string) =>
    page.evaluate(
      `(()=>{const a=document.querySelector(${JSON.stringify(selector)});if(!a)throw Error('missing input');Object.getOwnPropertyDescriptor(a.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,'value').set.call(a,${JSON.stringify(text)});a.dispatchEvent(new Event('input',{bubbles:true}))})()`,
    );
  const visit = async (search: string) => {
    await source!.call("Page.navigate", { url: `http://127.0.0.1:${port}/${search}` });
    await source!.until(`document.querySelector('[aria-label="Open in new window"]')`);
    if (search.includes("open=root")) await source!.until(`document.querySelector('textarea')`);
    await delay(150);
  };
  const connectPopup = async (previous: Set<string>) => {
    let target: Awaited<ReturnType<typeof pages>>[number] | undefined;
    const end = Date.now() + 15000;
    while (!target && Date.now() < end) {
      target = (await pages()).find((p) => p.url.includes("handoff=") && !previous.has(p.id));
      if (!target) await delay(30);
    }
    assert(target, "real popup target missing");
    popup = new CDP(target.webSocketDebuggerUrl);
    await popup.call("Page.enable");
    assert.equal(await popup.evaluate("window.opener === null"), true);
    return popup;
  };
  const move = async () => {
    const reply = await source!.evaluate(`!!document.querySelector('textarea')`);
    const previous = new Set((await pages()).map((p) => p.id));
    await source!.evaluate(`document.querySelector('[aria-label="Open in new window"]').click()`);
    const child = await connectPopup(previous);
    await child.until(
      `document.querySelector('.stream') && [...document.querySelectorAll('button')].some(n=>n.textContent==='Return to main window')`,
    );
    await source!.until(
      `!document.querySelector('.stream') && document.body.textContent.includes('This thread is in its own window')`,
    );
    if (reply)
      await child.until(
        `document.querySelector('textarea') && document.querySelector('button[title="Edit recipients"]')`,
      );
    return child;
  };
  const returned = async (child: CDP, body?: string) => {
    await click(child, "Return to main window");
    await source!.until(`document.querySelector('.stream')`);
    if (body !== undefined) {
      await source!.until(`document.querySelector('textarea')?.value === ${JSON.stringify(body)}`);
      assert.equal(await source!.evaluate(`document.querySelector('textarea').value`), body);
    }
    await delay(850); // A full autosave debounce must not create a second copy on Return.
    child.socket.close();
    popup = undefined;
  };
  await source.call("Page.enable");

  // Pending unaccepted AddressField input must survive both attempted directions.
  await visit("?accountId=A&open=root");
  await source.evaluate(`document.querySelector('button[title="Edit recipients"]').click()`);
  await input(source, '[aria-label="to addresses"]', "pending-source@example.test");
  const count = (await pages()).length;
  await source.evaluate(`document.querySelector('[aria-label="Open in new window"]').click()`);
  await delay(150);
  assert.equal((await pages()).length, count);
  assert.equal(
    await source.evaluate(`document.querySelector('[aria-label="to addresses"]').value`),
    "pending-source@example.test",
  );
  await input(source, '[aria-label="to addresses"]', "");
  let child = await move();
  await child.evaluate(`document.querySelector('button[title="Edit recipients"]').click()`);
  await input(child, '[aria-label="cc addresses"]', "pending-popup@example.test");
  await click(child, "Return to main window");
  assert.equal(
    await child.evaluate(`document.querySelector('[aria-label="cc addresses"]').value`),
    "pending-popup@example.test",
  );
  assert.equal(await source.evaluate(`!!document.querySelector('textarea')`), false);
  await input(child, '[aria-label="cc addresses"]', "");
  await returned(child, "");
  assert.deepEqual(writes, []);

  // Save in popup, then fresh exact-revision Return. Include a rejected Return/retry.
  child = await move();
  await input(child, "textarea", "Saved from popup");
  await child.until(`document.body.textContent.includes('Discard draft')`);
  await delay(850);
  assert.deepEqual(writes, ["POST"]);
  await source.evaluate(`document.querySelector('[aria-label="Compose"]').click()`);
  await source.until(`document.querySelector('aside[aria-label="Compose email"]')`);
  await click(child, "Return to main window");
  await child.until(`document.body.textContent.includes('cannot accept')`);
  assert.equal(await child.evaluate(`document.querySelector('textarea').disabled`), false);
  await click(source, "Cancel");
  await source.until(`document.body.textContent.includes('This thread is in its own window')`);
  await returned(child, "Saved from popup");
  assert.deepEqual(writes, ["POST"]);

  // Provider revision drift before preparation refuses without dropping the source body.
  const previous = new Set((await pages()).map((p) => p.id));
  const acknowledged = { ...draft! };
  await source.evaluate(`document.querySelector('[aria-label="Open in new window"]').click()`);
  draft = { ...draft!, messageId: "external-m2", body: "External provider words" };
  child = await connectPopup(previous);
  await child.until(
    `document.body.textContent.includes('could not be checked') || document.body.textContent.includes('cancelled')`,
  );
  assert.equal(
    await source.evaluate(`document.querySelector('textarea').value`),
    "Saved from popup",
  );
  assert.equal(await child.evaluate(`!!document.querySelector('textarea')`), false);
  draft = acknowledged;
  await child.call("Page.close");
  child.socket.close();
  popup = undefined;

  // A real saved transfer, edit, second save and Return use the new revision only once.
  child = await move();
  assert.equal(
    await child.evaluate(`document.querySelector('textarea').value`),
    "Saved from popup",
  );
  await input(child, "textarea", "Edited inside saved popup");
  await delay(1000);
  assert.deepEqual(writes, ["POST", "PUT"]);
  await returned(child, "Edited inside saved popup");
  assert.deepEqual(writes, ["POST", "PUT"]);

  // Blank managed popup -> same-thread existing draft card never escapes the managed host.
  await visit("?accountId=A&open=root");
  child = await move();
  await click(child, "Open draft");
  await child.until(`document.querySelector('textarea')?.value==='Edited inside saved popup'`);
  assert.equal(
    await child.evaluate(`String(JSON.parse(new URL(location.href).searchParams.get('popup')))`),
    "1",
  );
  assert.equal(await child.evaluate(`new URL(location.href).searchParams.has('handoff')`), true);
  await returned(child, "Edited inside saved popup");

  // Both inbox/search remain navigable at a narrow width while the registry stays away.
  await source.call("Emulation.setDeviceMetricsOverride", {
    width: 500,
    height: 900,
    deviceScaleFactor: 1,
    mobile: false,
  });
  for (const [search, back] of [
    ["?accountId=A&open=root", "List"],
    ["?q=synthetic&accountId=A&open=root", "Results"],
  ]) {
    await visit(search!);
    child = await move();
    await click(source, back!);
    await source.until(
      `!new URL(location.href).searchParams.has('open') && document.querySelector('li[data-root="root"]')?.getClientRects().length`,
    );
    await source.evaluate(`document.querySelector('li[data-root="root"] > button').click()`);
    await source.until(`document.body.textContent.includes('This thread is in its own window')`);
    assert.equal(await source.evaluate(`!!document.querySelector('textarea')`), false);
    await returned(child, "");
  }

  // Reader-only scope stays explicit, including ordinary unscoped Search and All mail.
  for (const search of [
    "?accountId=A&open=reader",
    "?q=synthetic&open=reader",
    "?label=&open=reader",
  ]) {
    const before = chainScopes.length;
    await visit(search);
    await source.until(`document.querySelector('.stream') && !document.querySelector('textarea')`);
    child = await move();
    assert.equal(await child.evaluate(`!!document.querySelector('textarea')`), false);
    await returned(child);
    if (!search.includes("accountId")) {
      assert.equal(
        await source.evaluate(`new URL(location.href).searchParams.get('accountId')`),
        "",
      );
      assert(chainScopes.slice(before).every((scope) => scope === null || scope === ""));
    }
  }

  // Native beforeunload cancellation retains ownership; actual close uses pagehide fresh return.
  await visit("?accountId=A&open=root");
  child = await move();
  await input(child, "textarea", "Created before native close");
  await delay(1000);
  const beforeClose = writes.length;
  child.acceptDialogs = false;
  await child.call("Page.close");
  await child.until(`document.querySelector('textarea')?.value==='Created before native close'`);
  assert(child.dialogs.includes("beforeunload"), "real native beforeunload was not exercised");
  assert.equal(await source.evaluate(`!!document.querySelector('textarea')`), false);
  child.acceptDialogs = true;
  await child.call("Page.close");
  await source.until(`document.querySelector('textarea')?.value==='Created before native close'`);
  await delay(850);
  assert.equal(writes.length, beforeClose);
  child.socket.close();
  popup = undefined;
  // A native close may be refused by another main editor, but its current owner is retained.
  for (const discarded of [false, true]) {
    await visit(`?accountId=A&open=root${discarded ? "&draft=d1" : ""}`);
    child = await move();
    if (discarded) {
      await click(child, "Discard draft");
      await child.until(`document.querySelector('textarea')?.value===''`);
      assert.equal(draft, undefined);
    } else {
      await input(child, "textarea", "Known owner after blocked native admission");
      await delay(1000);
    }
    const beforeRecovery: number = writes.length;
    await source.evaluate(`document.querySelector('[aria-label="Compose"]').click()`);
    await source.until(`document.querySelector('aside[aria-label="Compose email"]')`);
    await child.call("Page.close");
    await source.until(
      `document.body.textContent.includes('Close other editors before returning')`,
    );
    await click(source, "Cancel");
    await source.until(`document.body.textContent.includes('This thread is in its own window')`);
    await click(source, "Reopen saved state");
    await source.until(
      `document.querySelector('textarea')?.value===${JSON.stringify(discarded ? "" : "Known owner after blocked native admission")}`,
    );
    assert.equal(
      await source.evaluate(`new URL(location.href).searchParams.get('draft')`),
      discarded ? null : "d1",
    );
    await delay(850);
    assert.equal(writes.length, beforeRecovery);
    child.socket.close();
    popup = undefined;
  }

  // Query-only local input gets the same real beforeunload cancel/confirm protection as body text.
  await visit("?accountId=A&open=root");
  child = await move();
  await child.evaluate(`document.querySelector('button[title="Edit recipients"]').click()`);
  await input(child, '[aria-label="cc addresses"]', "unaccepted-native@example.test");
  const beforeQueryClose = writes.length;
  child.acceptDialogs = false;
  await child.call("Page.close");
  await child.until(
    `document.querySelector('[aria-label="cc addresses"]')?.value==='unaccepted-native@example.test'`,
  );
  assert(child.dialogs.includes("beforeunload"), "query-only input had no native cancel dialog");
  assert.equal(await source.evaluate(`!!document.querySelector('textarea')`), false);
  child.acceptDialogs = true;
  await child.call("Page.close");
  await source.until(`document.body.textContent.includes('without an acknowledged return')`);
  await click(source, "Reopen saved state");
  await source.until(`document.querySelector('textarea')?.value===''`);
  await delay(850);
  assert.equal(writes.length, beforeQueryClose);
  child.socket.close();
  popup = undefined;
  assert(reads.every((path) => !path.includes("http")));
  console.log(
    "PASS: built app real Chromium two-page revision-bound move/edit-save/Return, refusal-retry, managed scope, narrow navigation, scoped/unscoped readers, blocked native-close current-owner recovery, body/query-only native cancel/confirm; no duplicate saves after a full debounce.",
  );
} catch (error) {
  console.error("Smoke failure:", error);
  console.error("Fixture requests:", reads);
  if (source) console.error("Source DOM:", await source.evaluate("document.body?.innerText"));
  if (popup)
    console.error(
      "Popup DOM:",
      await popup.evaluate("document.body?.innerText").catch(() => "closed"),
    );
  throw error;
} finally {
  source?.socket.close();
  popup?.socket.close();
  browser.kill("SIGTERM");
  await new Promise<void>((resolve) => {
    if (browser.exitCode !== null) resolve();
    else browser.once("exit", () => resolve());
  });
  await new Promise<void>((resolve) => server.close(() => resolve()));
  await rm(profile, { recursive: true, force: true, maxRetries: 20, retryDelay: 100 });
}
