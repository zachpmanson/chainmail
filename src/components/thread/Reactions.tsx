import type { Entry } from "../../lib/timeline/spec";

/** Native disclosure also works in a script-free export. Identity is a From claim. */
export default function Reactions({ reactions }: { reactions?: Entry["reactions"] }) {
  if (!reactions?.length) return null;
  return (
    <div className="mt-2 flex flex-wrap items-start gap-2" aria-label="Received reactions">
      {reactions.map(({ emoji, events }) => (
        <details className="min-w-0 rounded-md border border-line px-2 py-1" key={emoji}>
          <summary className="cursor-pointer text-sm focus-visible:outline focus-visible:outline-offset-2 focus-visible:outline-accent">
            <span>{emoji}</span>{" "}
            <span aria-label={`${events.length} reaction notifications`}>{events.length}</span>
          </summary>
          <p className="my-1 text-2xs text-muted">
            Sender-supplied notifications, not verified intent.
          </p>
          {events.map((event) => (
            <details className="my-1 min-w-0 text-xs" key={event.sourceExtId}>
              <summary className="cursor-pointer wrap-anywhere focus-visible:outline focus-visible:outline-offset-2 focus-visible:outline-accent">
                {event.claimedFrom || "Unknown From"} · {event.date || "Unknown date"} · Original
                notification
              </summary>
              <p className="my-1 wrap-anywhere">Source: {event.sourceExtId}</p>
              <pre className="max-w-full overflow-auto wrap-anywhere whitespace-pre-wrap">
                {event.body}
              </pre>
              <a
                href={
                  event.sourceLink.startsWith("https://mail.google.com/mail/#all/")
                    ? event.sourceLink
                    : undefined
                }
                target="_blank"
                rel="noreferrer"
              >
                Open source in Gmail
              </a>
            </details>
          ))}
        </details>
      ))}
    </div>
  );
}
