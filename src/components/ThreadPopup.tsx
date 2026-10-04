import { $api } from "../lib/api";
import { ThreadPane } from "./ThreadPane";

/** A popup-sized, thread-only reading surface: **the reading pane itself**, in a
 * window of its own. The URL carries the stable thread id so it can be reloaded
 * without bringing up the inbox or the site navigation.
 *
 * The pane is reused rather than re-described (see ThreadPane): the same head,
 * the same controls, the same bubbles, so a thread read here is the thread read
 * beside the list rather than a second surface that drifts from it. What the
 * popup says is where the pane's way back goes — the list "← List" returns to is
 * the window behind this one, so its own close is what the control does.
 *
 * The pane draws its head from what the caller knows about the thread, and a
 * popup opened from an address knows only the id. The chain read — the same one
 * ThreadMessages makes, so one request — is what names the thread in the head. */
export function ThreadPopup({ rootExtId }: { rootExtId: string }) {
  const fetched = $api.useQuery("get", "/v1/chains/{rootExtId}", {
    params: { path: { rootExtId } },
  });
  const subject = fetched.data?.entries?.find((entry) => entry.subject)?.subject;

  return (
    <main className="thread-popup">
      <ThreadPane
        thread={{ rootExtId, ...(subject ? { subject } : {}) }}
        label="The thread"
        backLabel="Close"
        empty="No thread was specified."
        onClose={() => window.close()}
        // A second window of itself is not an offer this window can make: the
        // pane's own control is suppressed rather than duplicated.
        openInWindow={false}
      />
    </main>
  );
}
