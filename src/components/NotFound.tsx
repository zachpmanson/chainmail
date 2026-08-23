import { Link, useRouterState } from "@tanstack/react-router";

/**
 * The client's 404. The server answers every non-/v1/ path with the shell —
 * the client is the only thing that knows all the routes — so a mistyped or
 * stale URL lands here and fails loudly instead of resolving to the blank home
 * page.
 */
export function NotFound() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  return (
    <div className="wrap">
      <header className="top">
        <h1>chainmail</h1>
      </header>
      <p style={{ padding: "2rem", color: "var(--muted)" }}>
        No page at <code>{pathname}</code> — it is not a route.{" "}
        <Link to="/">Search the corpus</Link>.
      </p>
    </div>
  );
}