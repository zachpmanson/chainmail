import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Link,
  Outlet,
  useRouterState,
} from "@tanstack/react-router";
import { useEffect, useState } from "react";
import type { Timeline } from "./lib/spec";
import { loadSpec } from "./lib/loadSpec";
import { normalise } from "./lib/normalise";
import { $api } from "./lib/api";
import { SelectView } from "./components/Select";
import { ViewPage } from "./components/ViewPage";
import { NotFound } from "./components/NotFound";
import { Rendered } from "./components/Rendered";
import { StatusView } from "./components/StatusView";
import { SpecsView } from "./components/SpecsView";
import { OpsView } from "./components/OpsView";
import type { SearchMode } from "./lib/api";

/**
 * The two real routes of the app, owned entirely by the client:
 *
 *   "/"            — search, choose, build. Its search parameters (q, mode,
 *                    person, since) ARE the home page: they live in the URL so
 *                    Back from a built page restores them, and a reload restores
 *                    them too. The params are optional-typed so a partial or
 *                    empty search is a valid URL; validation applies the
 *                    defaults when they are read.
 *   "/view/<name>" — a page POST /v1/spec saved under that name, reloadable by
 *                    the URL alone. The server answers any /view/* path with
 *                    the shell; everything deeper is this route's business.
 *   "/status"      — which backends the corpus reads through are logged in.
 *   "/specs"      — every page saved under /view/<name>, newest first, so a
 *                     saved build can be reopened without remembering its name.
 *   "/ops"        — the people-merge review surface: the dedupe plan, shown
 *                     with the evidence, applied one pair at a time behind a
 *                     confirm. Read-only here means read-only there.
 *   "*"            — the client's own 404. Unknown paths reach the shell too,
 *                    so the client (which knows every route) is the one that
 *                    can truthfully say "no page here".
 *
 * Two legacy ways in survive on top of the routes, owned by the root layout:
 * ?spec=<file> loads a static spec from disk (the static pipeline's output,
 * the fixtures, vite's /@fs), and a spec dropped on the page is a transient
 * page that owns no URL. Both take over the whole screen while set — the URL
 * does not pretend to name them. ?spec= is deliberately declared on no route:
 * it is read from the router's location, so it passes through on any path,
 * and no route's search schema has to know about it.
 */

/** The search route's parameters. Optional, because the URL may name any
 * subset; the validators apply "" and "hybrid" as the defaults when read. */
export interface SearchParams {
  q?: string;
  mode?: SearchMode;
  person?: string;
  since?: string;
}

const isMode = (m: unknown): m is SearchMode =>
  m === "lexical" || m === "semantic" || m === "hybrid";

function validateSearchParams(search: Record<string, unknown>): SearchParams {
  return {
    q: typeof search.q === "string" ? search.q : undefined,
    mode: isMode(search.mode) ? search.mode : undefined,
    person: typeof search.person === "string" ? search.person : undefined,
    since: typeof search.since === "string" ? search.since : undefined,
  };
}

/**
 * The sign-in banner. A fresh install has no Google token yet; rather than a
 * dead-end "needs auth" the whole app stays usable and this bar offers the
 * step that unlocks the hourly slurp. Signed-in state is the same store the
 * server's own /auth/status reports, so a completed login flips it on its
 * own refetch.
 */
function SignInBar() {
  const auth = $api.useQuery("get", "/auth/status", {});
  if (auth.isPending || auth.isError) return null;
  if (auth.data?.signed_in) return null;
  return (
    <div className="authbar">
      Not signed in to Google — the hourly slurp is paused.{" "}
      <a href="/auth/login">Sign in with Google</a>.
    </div>
  );
}

/** The full screen is the app shell; this root owns the legacy ways in. */
function RootLayout() {
  // ?spec= is read from the router's location, not declared on any route, so
  // it passes through on any path without a route schema having to know it.
  const specParam = useRouterState({
    select: (s) => (s.location.search as Record<string, unknown>).spec,
  });
  const [specFile, setSpecFile] = useState<Timeline | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [dropped, setDropped] = useState<Timeline | null>(null);
  // Where the dropped spec was dropped, decided when it lands: a page dropped
  // on the search route gets a way back ("← choose chains"), one dropped on a
  // page route just is.
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  // ?spec=<file>: load the static spec once per URL. A blanking spec param
  // (navigation away) clears it, and an in-flight load is cancelled rather
  // than racing the next one.
  useEffect(() => {
    if (!specParam) {
      setSpecFile(null);
      return;
    }
    let cancelled = false;
    loadSpec(String(specParam))
      .then((sp) => {
        if (!cancelled) {
          setSpecFile(sp);
          setError(null);
        }
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [specParam]);

  // A spec dropped on the page is a transient page that owns no URL of its
  // own; it joins under whatever route is current.
  useEffect(() => {
    const onDrop = async (ev: DragEvent) => {
      ev.preventDefault();
      const f = ev.dataTransfer?.files?.[0];
      if (f) {
        try {
          setDropped(normalise(JSON.parse(await f.text())));
          setError(null);
        } catch (e) {
          setError(String(e));
        }
      }
    };
    const stop = (ev: DragEvent) => ev.preventDefault();
    addEventListener("drop", onDrop);
    addEventListener("dragover", stop);
    return () => {
      removeEventListener("drop", onDrop);
      removeEventListener("dragover", stop);
    };
  }, []);

  if (error)
    return (
      <pre style={{ padding: "2rem", whiteSpace: "pre-wrap", color: "crimson" }}>
        {error}
        {"\n\nOr drop a spec JSON onto the page."}
      </pre>
    );
  if (specFile) return <Rendered spec={specFile} />;
  if (dropped)
    return (
      <Rendered spec={dropped} onBack={pathname === "/" ? () => setDropped(null) : undefined} />
    );
  return (
    <>
      {/* The site nav is the header: the same cross-links that used to sit in
          the footer, at the top of every page instead — above the sign-in
          banner and the page's own header, so it is the first thing read and
          the one place site-level navigation lives. */}
      <header className="sitehead">
        <Link to="/">Home</Link>
        <span className="sep">·</span>
        <Link to="/specs">Browse</Link>
        <span className="sep">·</span>
        <Link to="/status">Services</Link>
        <span className="sep">·</span>
        <Link to="/ops">Ops</Link>
      </header>
      <SignInBar />
      <Outlet />
    </>
  );
}

const rootRoute = createRootRoute({
  component: RootLayout,
  // Unmatched paths are the client's 404: the server answers every non-`/v1/`
  // path with the shell, and only the client knows all the routes, so it is the
  // one that can truthfully say no page lives here. The old catch-all route
  // (`path: "*"`) rendered the router's default `<p>Not Found</p>` instead, so the
  // not-found component is registered on the root route, not as a leaf.
  notFoundComponent: NotFound,
});

const searchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  validateSearch: validateSearchParams,
  component: SelectView,
});

const statusRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/status",
  component: StatusView,
});

const specsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/specs",
  component: SpecsView,
});

const opsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/ops",
  component: OpsView,
});

const viewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/view/$name",
  component: ViewPage,
});

const routeTree = rootRoute.addChildren([searchRoute, statusRoute, specsRoute, opsRoute, viewRoute]);

/** The app's router, bound to the browser's history. */
export const router = createRouter({
  routeTree,
  defaultPreload: false,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

/**
 * A fresh router for tests: memory history (jsdom's window.history is shared
 * across a file, and a singleton router would keep its first URL forever), so
 * each test starts from its own initial URL and can assert on
 * router.state.location.
 */
export function createChainmailRouter(initialEntries: string[]) {
  return createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries }),
    defaultPreload: false,
  });
}