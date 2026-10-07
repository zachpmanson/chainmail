import { ApiError } from "../api/api";

// Not a security boundary: the server sanitises the html (spec.OriginalBody); a
// shadow root encapsulates styles but does not sandbox script.

// Cached per session as promises; rejections are evicted so a transient failure isn't sticky.
const asked = new Map<string, Promise<string>>();

export function fetchOriginal(extID: string): Promise<string> {
  const held = asked.get(extID);
  if (held) return held;
  const fetching = ask(extID);
  asked.set(extID, fetching);
  fetching.catch(() => asked.delete(extID));
  return fetching;
}

async function ask(extID: string): Promise<string> {
  const res = await fetch(`/v1/entries/${encodeURIComponent(extID)}/original`);
  if (!res.ok) throw new ApiError(res.status, await whyNot(res));
  return res.text();
}

// This route sits outside the generated client, so it mirrors the client's error-message extraction.
async function whyNot(res: Response): Promise<string> {
  const body = await res.text();
  try {
    const parsed = JSON.parse(body) as { error?: unknown };
    if (typeof parsed.error === "string" && parsed.error.trim() !== "") return parsed.error;
  } catch {
    // Not JSON: whatever arrived is the message.
  }
  const text = body.trim();
  if (text !== "") return text;
  return res.statusText.trim() !== "" ? `${res.status} ${res.statusText}` : `HTTP ${res.status}`;
}

// attachShadow throws if called twice, so reuse an existing root.
export function mountOriginal(host: HTMLElement, html: string): void {
  const root = host.shadowRoot ?? host.attachShadow({ mode: "open" });
  root.innerHTML = html;
  dropSchemeVariants(root);
}

/**
 * Drops `prefers-color-scheme` rules: inside a shadow root they follow the app's
 * dark mode, so senders' light-ink dark variants render unreadably on the white canvas.
 */
export function dropSchemeVariants(root: { styleSheets?: ArrayLike<CSSStyleSheet> | null }): void {
  for (const sheet of Array.from(root.styleSheets ?? [])) dropFrom(sheet);
}

function dropFrom(group: CSSStyleSheet | CSSGroupingRule): void {
  const rules = group.cssRules;
  // Backwards: deleting a rule shifts every rule after it.
  for (let i = rules.length - 1; i >= 0; i--) {
    // Duck-typed so `@supports` and other grouping rules are walked too.
    const rule = rules[i] as CSSRule & { cssRules?: CSSRuleList; conditionText?: string };
    if (!rule.cssRules) continue;
    if (/prefers-color-scheme/i.test(rule.conditionText ?? "")) {
      group.deleteRule(i);
      continue;
    }
    dropFrom(rule as unknown as CSSGroupingRule);
  }
}
