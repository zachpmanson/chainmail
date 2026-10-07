import {
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  Link,
  Outlet,
  useRouterState,
  useSearch,
} from "@tanstack/react-router";
import { lazy, Suspense, useEffect, useState } from "react";
import { PencilSquareIcon } from "@heroicons/react/24/outline";
import type { Timeline } from "./lib/timeline/spec";
import { normalise } from "./lib/timeline/normalise";
import SelectView from "./components/inbox/SelectView";
import Inbox from "./components/inbox/Inbox";
import ThreadPopup from "./components/thread/ThreadPopup";
import NotFound from "./components/NotFound";
import { validateOpsTab } from "./lib/ops/opsTabs";
import DeployStamp from "./components/navigation/DeployStamp";
import ToastHost from "./components/ui/ToastHost";
import NavReading from "./components/navigation/NavReading";
import NavRefresh from "./components/navigation/NavRefresh";
import AutoRefresh from "./components/navigation/AutoRefresh";
import NavSearch from "./components/navigation/NavSearch";
import ComposeProvider from "./components/compose/ComposeProvider";
import IconButton from "./components/ui/IconButton";
import type { SearchMode } from "./lib/api/api";

const Rendered = lazy(() => import("./components/specs/Rendered"));

function Loading() {
  return <p className="px-5 py-4 text-xs text-muted">Loading…</p>;
}

/** The server answers every non-/v1/ path with the shell; all routing is client-side. */

/** All optional; the validators apply the defaults when read. */
export interface SearchParams {
  q?: string;
  mode?: SearchMode;
  person?: string;
  since?: string;
  /** Limit the inbox/search to copies in one connected Gmail account. */
  accountId?: string;
  label?: string;
  open?: string;
  /** Render a focused thread-only page intended for a script-sized popup. */
  popup?: string;
}

const isMode = (m: unknown): m is SearchMode =>
  m === "lexical" || m === "semantic" || m === "hybrid";

function validateSearchParams(search: Record<string, unknown>): SearchParams {
  return {
    q: typeof search.q === "string" ? search.q : undefined,
    mode: isMode(search.mode) ? search.mode : undefined,
    person: typeof search.person === "string" ? search.person : undefined,
    since: typeof search.since === "string" ? search.since : undefined,
    accountId: typeof search.accountId === "string" ? search.accountId : undefined,
    label: typeof search.label === "string" ? search.label : undefined,
    open: typeof search.open === "string" ? search.open : undefined,
    // TanStack's default search parser JSON-decodes numeric values, so the
    // URL `?popup=1` arrives as the number 1 rather than the string "1".
    popup: search.popup === 1 ? "1" : typeof search.popup === "string" ? search.popup : undefined,
  };
}

/** The full screen is the app shell; this root owns the legacy way in. */
function RootLayout() {
  const [composing, setComposing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [dropped, setDropped] = useState<Timeline | null>(null);
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const popup = useRouterState({
    select: (s) => {
      const popup = (s.location.search as Record<string, unknown>).popup;
      return popup === "1" || popup === 1;
    },
  });

  // A dropped spec is a transient page that owns no URL.
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
  if (dropped)
    return (
      <Suspense fallback={<Loading />}>
        <Rendered spec={dropped} onBack={pathname === "/" ? () => setDropped(null) : undefined} />
      </Suspense>
    );
  return (
    <>
      {!popup ? (
        <>
          <header className="sitehead max-w-none m-0 flex flex-wrap items-center border-b border-line px-5 pt-4 pb-3 text-sm text-muted">
            <nav className="sitenav relative flex min-w-0 flex-1 flex-nowrap items-center overflow-x-auto overflow-y-hidden h-9">
              <Link
                to="/"
                className="text-fg text-sm font-bold tracking-[-.01em] no-underline hover:text-accent"
              >
                chainmail
              </Link>
              <span className="mx-2 text-line">·</span>
              <Link to="/specs">Braids</Link>
              <span className="mx-2 text-line">·</span>
              <Link to="/settings">Settings</Link>
              <span className="mx-2 text-line">·</span>
              <Link to="/ops">Ops</Link>
              <NavReading />
              <span className="navright ml-auto flex flex-nowrap items-center justify-end gap-4 pr-52">
                <DeployStamp />
                <AutoRefresh />
                <NavRefresh />
                <IconButton aria-label="Compose" title="Compose" onClick={() => setComposing(true)}>
                  <PencilSquareIcon aria-hidden="true" />
                </IconButton>
                <NavSearch />
              </span>
            </nav>
            <div className="buildslot not-empty:grow not-empty:basis-full" />
          </header>
        </>
      ) : null}
      <ComposeProvider composing={composing} closeCompose={() => setComposing(false)}>
        <Outlet />
      </ComposeProvider>
      <ToastHost />
    </>
  );
}

const rootRoute = createRootRoute({
  component: RootLayout,
  // Registered on the root, not as a `path: "*"` leaf, which renders the router's default 404.
  notFoundComponent: NotFound,
});

/** With q, person or since it's the selection stage; otherwise the inbox. */
function Home() {
  const urlSearch = useSearch({ from: "/" });
  if (urlSearch.popup === "1") {
    return urlSearch.open ? (
      <ThreadPopup rootExtId={urlSearch.open} />
    ) : (
      <main>
        <p>No thread was specified.</p>
      </main>
    );
  }
  // `?q=` or whitespace is a cleared box, not a search; `mode`, `label`, `open` alone don't count.
  const asking = Boolean(
    urlSearch.q?.trim() || urlSearch.person?.trim() || urlSearch.since?.trim(),
  );
  return asking ? <SelectView /> : <Inbox />;
}

const searchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  validateSearch: validateSearchParams,
  component: Home,
});

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/settings",
  component: lazyRouteComponent(() => import("./components/settings/SettingsView")),
});

const specsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/specs",
  component: lazyRouteComponent(() => import("./components/specs/SpecsView")),
});

const opsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/ops",
  validateSearch: validateOpsTab,
  component: lazyRouteComponent(() => import("./components/ops/OpsView")),
});

const viewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/view/$name",
  component: lazyRouteComponent(() => import("./components/specs/ViewPage")),
});

const routeTree = rootRoute.addChildren([
  searchRoute,
  settingsRoute,
  specsRoute,
  opsRoute,
  viewRoute,
]);

/** The app's router, bound to the browser's history. */
export const router = createRouter({
  routeTree,
  defaultPreload: false,
  defaultPendingComponent: Loading,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
