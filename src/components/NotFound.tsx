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
    <div className="mx-auto max-w-[76rem] px-5 pt-7 pb-14">
      <header className="mb-[.25rem] border-b border-line pb-[.7rem]">
        <h1>chainmail</h1>
      </header>
      <p className="p-8 text-muted">
        No page at <code>{pathname}</code> — it is not a route.{" "}
        <Link to="/">Search the corpus</Link>.
      </p>
    </div>
  );
}
