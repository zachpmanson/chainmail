/**
 * Names for saved pages. The router lives in src/router.tsx; the two routes it
 * owns (/ and /view/<name>) are the whole surface, so a URL is only ever one
 * pathname branch plus a search object — no router ceremony here. What remains
 * are the ways a page gets its name, shared by whoever builds one.
 */

/** A page title, reduced to what POST /v1/spec will accept as a name. */
export function slug(title: string): string {
  return (
    title
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 64) || ""
  );
}

/**
 * A name for a build that has no title. Clock-based, so two unnamed builds can
 * never collide: the spec is a saved page, and overwriting one is losing one.
 */
export function untitledName(): string {
  return `spec-${Date.now().toString(36)}`;
}