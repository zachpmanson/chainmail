import { useRef } from "react";
import { useNavigate } from "@tanstack/react-router";
import { $api } from "../api/api";
import { slug, untitledName } from "./route";

export interface PageRequest {
  /** Root ext ids of the chains to put on the page, in the order they were chosen. */
  chains: string[];
  /** Optional human title. The saved name is derived from it. */
  title?: string;
  /** The reader's own addresses; the corpus doesn't record which mailbox it came from. */
  me?: string[];
  /** Searches to record for later refresh; inbox builds record none. */
  queries?: { q: string; note?: string }[];
}

export function useBuildPage() {
  const navigate = useNavigate();
  const pendingName = useRef("");
  const build = $api.useMutation("post", "/v1/spec", {
    // The server saves exactly `name` but may return a borrowed subject as the
    // title, so navigate by the request name, never a reslugged title.
    onSuccess: () =>
      navigate({
        to: "/view/$name",
        params: { name: pendingName.current },
      }),
  });

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
