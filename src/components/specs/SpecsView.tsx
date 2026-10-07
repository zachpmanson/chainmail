import { Link } from "@tanstack/react-router";
import { $api } from "../../lib/api/api";
import { when } from "../../lib/ui/stamp";
import { errText } from "../../lib/ui/errText";
import InlineAlert from "../ui/InlineAlert";

export default function SpecsView() {
  const list = $api.useQuery("get", "/v1/specs", {});

  return (
    <div className="wrap mx-0 w-full max-w-none px-5 pt-7 pb-14">
      {list.isError ? <InlineAlert>{errText(list.error)}</InlineAlert> : null}

      {list.isFetching && !list.data ? (
        <p className="my-1.5 mb-2 text-xs text-muted">Reading the saved pages…</p>
      ) : null}

      {list.data && list.data.specs.length === 0 ? (
        <p className="my-1.5 mb-2 text-xs text-muted">
          No saved pages yet — build one from a <Link to="/">search</Link>, and it appears here.
        </p>
      ) : null}

      <ul className="mt-3 list-none border-t border-line p-0">
        {list.data?.specs.map((s) => (
          <li
            className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-line py-2"
            key={s.name}
          >
            <span className="min-w-32 text-sm font-semibold">
              <Link to="/view/$name" params={{ name: s.name }}>
                {s.title || s.name}
              </Link>
            </span>
            <span className="flex-[1_1_12rem] wrap-break-word text-xs text-muted">
              {when(s.savedAt)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
