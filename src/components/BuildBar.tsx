import { useState } from "react";
import { $api } from "../lib/api";
import { useBuildPage } from "../lib/build";
import { Failure } from "./ChainPreview";

/**
 * The bar that turns the ticked chains into a page: a title, and the button that
 * asks the service for it.
 *
 * One bar for both pages. It was written twice — at the foot of the inbox and at
 * the foot of the search results — and the two copies had already drifted: the
 * inbox's recorded the reader's addresses as a preference on the way past, the
 * search page's used them once and dropped them, so naming yourself while
 * building from the search left the inbox pane refusing to mark your own mail.
 * Which list the chains were ticked in is not a fact about what the bar does; the
 * bar appears once something is ticked, on both pages, and the rules about what a
 * build records live in one place.
 *
 * The reader's own addresses are **not** here. They are a setting, not a field
 * about the page being built: they decide which messages are marked as the
 * reader's wherever mail is read, including threads nobody ever built a page
 * from. They were in the bar only because the bar was the first place that needed
 * them. They are written on the services page, with the other settings, and read
 * from there by whoever needs them — here, and the reading pane.
 */
export function BuildBar({
  chosen,
  queries,
}: {
  /** Root ext ids of the chains to build from, in the order they were ticked. */
  chosen: string[];
  /**
   * The searches to record on the page, when a search is what found the chains,
   * so a later refresh can propose what the same query would find now. The inbox
   * passes none: no query found its chains, and a made-up one would have refresh
   * proposing threads nobody asked about.
   */
  queries?: { q: string; note?: string }[];
}) {
  const [title, setTitle] = useState("");
  const { build, start } = useBuildPage();
  // The reader's addresses, read where they are written. The build needs them to
  // mark the reader's own messages as theirs; nothing in the corpus records which
  // mailbox it was collected from, so they can only be told, never inferred.
  const settings = $api.useQuery("get", "/v1/settings", {});

  // Nothing ticked is nothing to build from, so there is no bar: a form with no
  // object is an instruction to do something with nothing.
  if (chosen.length === 0) return null;

  return (
    <div className="selbuild ibbuild">
      <label className="self">
        <span>Page title</span>
        <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="optional" />
      </label>
      <button
        type="button"
        disabled={build.isPending}
        onClick={() => start({ chains: chosen, title, me: settings.data?.me ?? [], queries })}
      >
        {build.isPending
          ? "Building…"
          : `Build page from ${chosen.length} chain${chosen.length === 1 ? "" : "s"}`}
      </button>
      {/* Seconds of silence reads as a broken page, so the wait says what it is
          waiting on and how much of it there is. */}
      {build.isPending ? (
        <p className="selnote" role="status">
          Recovering HTML and detecting boilerplate across {chosen.length} chain
          {chosen.length === 1 ? "" : "s"}. This takes a few seconds.
        </p>
      ) : null}
      {build.isError ? <Failure error={build.error} /> : null}
    </div>
  );
}
