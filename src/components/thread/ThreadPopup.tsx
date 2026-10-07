import { $api } from "../../lib/api/api";
import ThreadPane from "./ThreadPane";

/** The reading pane in its own window. It knows only the id, so its head comes from the chain read. */
export default function ThreadPopup({ rootExtId }: { rootExtId: string }) {
  const chain = $api.useQuery("get", "/v1/chains/{rootExtId}", {
    params: { path: { rootExtId } },
  });
  const summary = chain.data?.summary;

  return (
    <main className="flex h-screen flex-col [&_.ibread]:flex [&_.ibread]:flex-1 [&_.ibread]:flex-col [&_.ibread]:min-h-0 [&_.ibread-head]:flex-none [&>p]:mx-5 [&>p]:mt-2 [&>p]:text-[.78rem] [&>p]:text-muted">
      <ThreadPane
        thread={{
          rootExtId,
          subject: summary?.subject,
          entries: summary?.entries,
          people: summary?.people,
          attachments: summary?.attachments,
          unread: summary?.unread,
        }}
        label="The thread"
        backLabel="Close"
        empty="No thread was specified."
        onClose={() => window.close()}
        openInWindow={false}
      />
    </main>
  );
}
