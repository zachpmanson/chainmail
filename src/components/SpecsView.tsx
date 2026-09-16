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
        <p className="selfail" role="alert">
          {errText(list.error)}
        </p>
      ) : null}

      {list.isFetching && !list.data ? (
        <p className="stnote">Reading the saved pages…</p>
      ) : null}

      {list.data && list.data.specs.length === 0 ? (
        <p className="stnote">
          No saved pages yet — build one from a <Link to="/">search</Link>, and
          it appears here.
        </p>
      ) : null}

      <ul className="stlist">
        {list.data?.specs.map((s) => (
          <li className="strow" key={s.name}>
            <span className="stlabel">
              <Link to="/view/$name" params={{ name: s.name }}>
                {s.title || s.name}
              </Link>
            </span>
            <span className="stdetail">{when(s.savedAt)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}