import { Link } from "@tanstack/react-router";
import { $api } from "../lib/api";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/**
 * A saved-at stamp, as the server wrote it (UTC RFC3339), shown in the reader's
 * own local time. Two digits survive from the wire: date and clock. The year is
 * kept when it is not the current one, so a page months old does not read as if
 * it were recent; the seconds are dropped, because within an index they are
 * noise next to the date.
 */
function when(stamp: string): string {
  const d = new Date(stamp);
  if (Number.isNaN(d.getTime())) return stamp;
  const y = d.getFullYear();
  const nowY = new Date().getFullYear();
  const date = y === nowY
    ? d.toLocaleDateString(undefined, { day: "numeric", month: "short" })
    : d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
  const t = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  return `${date}, ${t}`;
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
      <header className="top">
        <h1>chainmail</h1>
        <p className="sub">
          Saved pages, newest first. Each name is its URL — pick one to reopen a
          build.
        </p>
      </header>

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