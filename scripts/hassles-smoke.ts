// Local-only browser smoke harness. Always mocked; never loads server credentials.
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { resolve, extname } from "node:path";
import { hasslesFixture } from "../test/hasslesFixture";

const fixture = hasslesFixture();
const root = resolve("cmd/server/dist");
const types: Record<string, string> = {
  ".js": "text/javascript",
  ".css": "text/css",
  ".html": "text/html",
  ".svg": "image/svg+xml",
};
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url ?? "/", "http://localhost").pathname;
    if (path.startsWith("/v1/") || path.startsWith("/auth/") || path === "/__hassles_fixture") {
      let body = "";
      for await (const part of req) {
        body += String(part);
        if (body.length > 1 << 20) throw new Error("Fixture request too large");
      }
      const result = fixture.handle(
        req.url ?? "/",
        req.method,
        body ? JSON.parse(body) : undefined,
      );
      res.writeHead(result.status, {
        "Content-Type": "application/json",
        "Cache-Control": "no-store",
      });
      res.end(JSON.stringify(result.data));
      return;
    }
    const file = path.startsWith("/assets/")
      ? resolve(root, `.${path}`)
      : resolve(root, "index.html");
    if (!file.startsWith(`${root}/`)) throw new Error("Invalid fixture path");
    res.writeHead(200, { "Content-Type": types[extname(file)] ?? "application/octet-stream" });
    res.end(await readFile(file));
  } catch {
    res.writeHead(400);
    res.end("Offline fixture request failed. Run npm run build first.");
  }
});
server.listen(8877, "127.0.0.1", () =>
  console.log("Offline Hassles fixture: http://127.0.0.1:8877/hassles (no upstream fallback)"),
);
