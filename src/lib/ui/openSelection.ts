import { parseTextSearch } from "./searchParams";

export const COMPOSE_OPEN = "compose";

/** The reserved compose selection must never become a corpus root. */
export function openThread(open: string | undefined): string | undefined {
  return open === COMPOSE_OPEN ? undefined : open;
}

/** History metadata is untrusted, and Close must stay within an app view. */
export function safeComposeOrigin(value: unknown): string | undefined {
  if (
    typeof value !== "string" ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    value.includes("\\") ||
    [...value].some((char) => char.charCodeAt(0) <= 0x20)
  )
    return undefined;
  const url = new URL(value, "https://app.invalid");
  if (url.origin !== "https://app.invalid") return undefined;
  if (!/^(?:\/|\/settings|\/specs|\/ops|\/view\/[^/]+)$/.test(url.pathname)) return undefined;
  const open = parseTextSearch(url.search).open;
  if (open === COMPOSE_OPEN || (Array.isArray(open) && open.includes(COMPOSE_OPEN)))
    return undefined;
  return `${url.pathname}${url.search}${url.hash}`;
}
