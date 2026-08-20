import { normalise } from "./normalise";
import type { Timeline } from "./spec";

declare const __HOME__: string;

/**
 * Resolve a ?spec= value to a fetchable URL.
 *
 * A browser cannot read the filesystem, so an absolute path is served through
 * Vite's /@fs/ route (dev only, and scoped by server.fs.allow). Paths served
 * from the fixtures directory still work as plain URLs, so try that first.
 */
export function candidates(param: string): string[] {
  let p = param.trim();
  if (p.startsWith("~/")) p = `${typeof __HOME__ === "string" ? __HOME__ : ""}${p.slice(1)}`;
  if (/^https?:\/\//.test(p) || p.startsWith("/@fs/")) return [p];
  if (p.startsWith("/")) return [p, `/@fs${p}`];
  return [`/${p}`, `/@fs/${p}`];
}

export async function loadSpec(param: string): Promise<Timeline> {
  const tried: string[] = [];
  for (const url of candidates(param)) {
    tried.push(url);
    let res: Response;
    try {
      res = await fetch(url);
    } catch {
      continue;
    }
    if (!res.ok) continue;
    const text = await res.text();
    // an SPA fallback can answer 200 with HTML; only JSON is a spec
    if (/^\s*</.test(text)) continue;
    return normalise(JSON.parse(text));
  }
  throw new Error(
    `Could not load a spec from ${param}\n\nTried:\n  ${tried.join("\n  ")}\n\n` +
      `Absolute paths are served via Vite's /@fs route and must sit inside\n` +
      `server.fs.allow (currently the repo and your home directory).`,
  );
}
