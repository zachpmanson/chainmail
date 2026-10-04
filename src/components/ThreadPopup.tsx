import { $api } from "../lib/api";
import { ThreadMessages } from "./ThreadMessages";

/** A popup-sized, thread-only reading surface. The URL carries the stable thread
 * id so it can be reloaded without bringing up the inbox or the site navigation.
 *
 * The head is the reading pane's arrangement rather than a document's: the
 * subject and Close are a row of the window, and the thread scrolls in its own
 * box beneath them (see .thread-popup-body). A long thread never takes the
 * subject off the screen, which is what the pane's own head does for the same
 * reason (see .ibread-head) — and it is a sibling of the scroll rather than a
 * sticky line inside it, for the reasons given there. */
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
      <div className="thread-popup-body">
        <ThreadMessages thread={{ rootExtId }} />
      </div>
    </main>
  );
}
