import { Link, useRouterState } from "@tanstack/react-router";

/** The server answers every non-/v1/ path with the shell, so unknown URLs land here. */
export default function NotFound() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  return (
    <div className="mx-auto max-w-[76rem] px-5 pt-7 pb-14">
      <header className="mb-1 border-b border-line pb-3">
        <h1>chainmail</h1>
      </header>
      <p className="p-8 text-muted">
        No page at <code>{pathname}</code> — it is not a route.{" "}
        <Link to="/">Search the corpus</Link>.
      </p>
    </div>
  );
}
