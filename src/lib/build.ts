import { useRef } from "react";
import { useNavigate } from "@tanstack/react-router";
import { $api } from "./api";
import { slug, untitledName } from "./route";

/**
 * Asking the service for a page. One owner for the three things that have to
 * agree: the name, the request, and the URL that gets pushed. Two views build
 * pages now — the search results and the inbox — and each of them naming its own
 * page would eventually disagree with the other about what a name is.
 *
 * The name is settled at click time, in one place, so the URL that is pushed and
 * the file the server saves can never disagree. A clock name for an untitled
 * page is generated per click for the same reason: two builds must not overwrite
 * each other silently.
 */
export interface PageRequest {
  /** Root ext ids of the chains to put on the page, in the order they were chosen. */
  chains: string[];
  /** Optional human title. The saved name is derived from it. */
  title?: string;
  /**
   * The reader's own addresses, so their outbound messages can be marked as
   * theirs. Nothing in the corpus records which mailbox it was collected from,
   * so this can only be told, never inferred.
   */
  me?: string[];
  /**
   * The searches to record on the page, so a later refresh can propose the
   * chains the same query would find now. A page built from the inbox records
   * none: no query found its chains, and a made-up one would have refresh
   * proposing threads nobody asked about.
   */
  queries?: { q: string; note?: string }[];
}

export function useBuildPage() {
  const navigate = useNavigate();
  const pendingName = useRef("");
  const build = $api.useMutation("post", "/v1/spec", {
    // The saved page's URL is the name the client chose, always: the server
    // saves exactly `name` from the request and returns it as the title (it may
    // borrow a subject when none was given, but that borrows a title, not a file
    // name). Reslugging the returned title would point the address bar at a file
    // that was never written — a titleless build saves under the clock name but
    // announces the borrowed subject's slug. The request name IS the saved file,
    // so it IS the URL.
    onSuccess: () =>
      navigate({
        to: "/view/$name",
        params: { name: pendingName.current },
      }),
  });

  /** Start a build. The response is normalised downstream, not here: a spec that
   * will not normalise is the renderer's error to report, where the person who
   * pressed the button expects to be told. */
  const start = (req: PageRequest) => {
    const title = req.title?.trim() ?? "";
    pendingName.current = slug(title) || untitledName();
    build.mutate({
      body: {
        chains: req.chains,
        name: pendingName.current,
        ...(title ? { title } : {}),
        ...(req.me?.length ? { me: req.me } : {}),
        ...(req.queries?.length ? { queries: req.queries } : {}),
      },
    });
  };

  return { build, start };
}
