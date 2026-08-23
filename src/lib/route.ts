/**
 * The two views of the app live at two URLs: / is search-and-select, and
 * /view/<name> is a page that POST /v1/spec saved under that name. That one
 * route prefix is the whole router — there is no router library, because a
 * pathname branch plus pushState/popstate is the entire surface. The hash
 * anchors Timeline behaviour uses (/#m-20260327-0650-cm) are fragments and
 * work under any path, so the router and the anchors stay out of each other's
 * way.
 */

export type Route = { view: "render"; name: string } | { view: "search" };

/** The render route, from the address bar. Unknown paths are the search route
 * (the server 404s them, so they never make it here as a page). */
export function parseRoute(): Route {
  const m = /^\/view\/([^/]+)\/?$/.exec(location.pathname);
  return m ? { view: "render", name: decodeURIComponent(m[1]!) } : { view: "search" };
}

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