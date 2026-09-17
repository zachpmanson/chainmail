import { useEffect } from "react";
import { $api, ApiError, type CorpusEntry } from "../lib/api";

/**
 * Names the status so two declines are told apart. "Not found" and "Rejected"
 * are different instructions to the person reading them: one means the id names
 * nothing, the other means the query has to change. A single "request failed"
 * would leave them guessing which.
 */
export function statusLabel(status: number): string {
  if (status === 404) return "Not found";
  if (status === 400) return "Rejected";
  if (status === 429) return "Busy";
  if (status === 503) return "Service unavailable";
  if (status === 504) return "Timed out";
  return `Error ${status}`;
}

export function Failure({ error }: { error: unknown }) {
  const api = error instanceof ApiError ? error : null;
  return (
    <p className="selfail" role="alert">
      <strong>{api ? `${statusLabel(api.status)} (${api.status})` : "Could not reach the service"}</strong>{" "}
      <span>{error instanceof Error ? error.message : String(error)}</span>
    </p>
  );
}

/** The wire's yyyy-mm-dd day is the part worth showing here; the clock is
 * the operator's own, left to them (same convention as the /status screen). */
function dayOf(stamp?: string): string {
  return stamp ? stamp.slice(0, 10) : "";
}

function EntryCard({ e }: { e: CorpusEntry }) {
  return (
    <div className="selen">
      <div className="selenh">
        {e.author ? <span className="selena">{e.author}</span> : <em className="selenun">unknown author</em>}
        {e.quoted ? (
          <span className="selenq" title="this entry survived only inside a quoted block, not as a standalone message">
            quoted
          </span>
        ) : null}
        <span className="selendate">{dayOf(e.ts)}</span>
      </div>
      {e.subject ? <div className="selensubj">{e.subject}</div> : null}
      {e.body ? <p className="selenbody">{e.body}</p> : <p className="selenempty">no text body</p>}
    </div>
  );
}

/** The thread fields the preview reads. Both a search hit (ChainHit) and a
 * refresh proposal (RefreshCandidate) carry them, so one modal serves the two
 * places a candidate thread needs judging before it is committed anywhere. */
export interface PreviewableThread {
  rootExtId: string;
  subject?: string;
  /** How many messages the thread holds. Absent when the caller knows only the
   *  id — the inbox read from an address bar, say — in which case no count is
   *  claimed rather than a zero. */
  entries?: number;
  /** How many people it holds, senders and recipients alike. Absent for the same
   *  reason as entries, and read by the head beside the message count. */
  people?: number;
  /** How many files it carries, over the whole thread. Absent for the same reason
   *  as entries — and drawn when absent, like the other two: the head shows what
   *  it was told and claims nothing. */
  attachments?: number;
  /** How many of its messages the mailbox still calls unread. Absent for the
   *  same reason as entries: a caller holding only an id cannot say, and the
   *  pane's read-state control hides rather than guesses. */
  unread?: number;
}

/** A candidate thread read as data. This is deliberately NOT the rendered
 * transcript: reading a thread is a different act from rendering one, and the
 * /v1/chains endpoint returns plain entries without paying for spec assembly.
 * It shows enough to judge a candidate before committing it to a page — who said
 * what, in what order — and it is the same reading in either place it appears:
 * the search page's modal, and the inbox's right-hand pane. */
export function ThreadReading({ thread }: { thread: PreviewableThread }) {
  const fetched = $api.useQuery("get", "/v1/chains/{rootExtId}", {
    params: { path: { rootExtId: thread.rootExtId } },
  });
  const entries = fetched.data?.entries ?? [];
  return (
    <>
      {fetched.isError ? <Failure error={fetched.error} /> : null}
      {fetched.isFetching ? <p className="selnote">Loading the thread…</p> : null}
      {!fetched.isFetching && !fetched.isError && entries.length === 0 ? (
        <p className="selnote">No entries to show.</p>
      ) : null}
      {entries.length > 0 ? (
        <div className="selpv-list">
          {entries.map((e) => (
            <EntryCard key={e.extId} e={e} />
          ))}
        </div>
      ) : null}
    </>
  );
}

/** The same reading, in a modal: for a candidate being judged from the search
 * page, where there is no room to put it beside the list. */
export function ThreadPreview({ thread, onClose }: { thread: PreviewableThread; onClose: () => void }) {
  // Escape closes the modal, matching the transcript's dialog habits; the
  // listener lives here because the modal only exists while it is open.
  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="selpv" role="dialog" aria-modal="true" aria-label="Thread preview" onClick={onClose}>
      <div className="selpv-panel" onClick={(e) => e.stopPropagation()}>
        <div className="selpv-head">
          <b>preview</b>
          <span className="note">{thread.subject || "(no subject)"}{thread.entries ? ` · ${thread.entries} entr${thread.entries === 1 ? "y" : "ies"}` : ""}</span>
          <button type="button" className="selpv-close" onClick={onClose}>
            Close
          </button>
        </div>
        <ThreadReading thread={thread} />
      </div>
    </div>
  );
}