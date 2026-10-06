import { Link } from "@tanstack/react-router";
import { $api } from "../lib/api";
import { when } from "../lib/stamp";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/**
 * The /specs route: the index of every page POST /v1/spec saved, newest first,
 * so a saved build can be reopened without remembering its name. Distinct pages
 * routinely share a title, so each row leans on the saved-at time to tell them
 * apart, and the whole name is the link — /view/<name> is what a saved page is.
 */
export function SpecsView() {
  const list = $api.useQuery("get", "/v1/specs", {});

  return (
    <div className="wrap statuswrap">
      {list.isError ? (
        <p className="selfail mt-[.7rem] rounded-md border border-line border-l-[3px] border-l-red-700 bg-card px-[.7rem] py-2 text-[.82rem]" role="alert">
          {errText(list.error)}
        </p>
      ) : null}

      {list.isFetching && !list.data ? (
        <p className="my-[.35rem] mb-2 text-[.74rem] text-[var(--muted)]">Reading the saved pages…</p>
      ) : null}

      {list.data && list.data.specs.length === 0 ? (
        <p className="my-[.35rem] mb-2 text-[.74rem] text-[var(--muted)]">
          No saved pages yet — build one from a <Link to="/">search</Link>, and
          it appears here.
        </p>
      ) : null}

      <ul className="mt-[.7rem] list-none border-t border-[var(--line)] p-0">
        {list.data?.specs.map((s) => (
          <li className="flex flex-wrap items-center gap-x-[.7rem] gap-y-[.45rem] border-b border-[var(--line)] py-[.55rem]" key={s.name}>
            <span className="min-w-32 text-[.8rem] font-semibold">
              <Link to="/view/$name" params={{ name: s.name }}>
                {s.title || s.name}
              </Link>
            </span>
            <span className="flex-[1_1_12rem] break-words text-[.72rem] text-[var(--muted)]">{when(s.savedAt)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}