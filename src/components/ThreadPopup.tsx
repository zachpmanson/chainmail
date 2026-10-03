import { $api } from "../lib/api";
import { ThreadMessages } from "./ThreadMessages";

/** A popup-sized, thread-only reading surface. The URL carries the stable thread
 * id so it can be reloaded without bringing up the inbox or the site navigation. */
export function ThreadPopup({ rootExtId }: { rootExtId: string }) {
  const fetched = $api.useQuery("get", "/v1/chains/{rootExtId}", {
    params: { path: { rootExtId } },
  });
  const subject = fetched.data?.entries?.find((entry) => entry.subject)?.subject;

  return (
    <main className="thread-popup">
      <header className="thread-popup-head">
        <h1>{subject || "Thread"}</h1>
        <button type="button" onClick={() => window.close()}>
          Close
        </button>
      </header>
      <ThreadMessages thread={{ rootExtId }} />
    </main>
  );
}
