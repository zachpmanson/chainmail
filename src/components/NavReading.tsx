import { useIsFetching } from "@tanstack/react-query";

/**
 * "Reading the corpus…", in the nav, between the links and the build stamp.
 *
 * Every view here is a question put to the same corpus, and the answer is
 * fetched rather than held, so the page the reader is looking at is not the one
 * being read. A page that said this itself said it in the middle of its own
 * content — where the reading is the reason the content is missing, and where
 * the notice shoved the content down as it came and went. It is a fact about the
 * site, so it sits where the site's other fact, the build stamp, already does.
 *
 * `useIsFetching` rather than one view's `isPending`: the status is about the
 * client waiting on the service at all, and a per-view flag would have to be
 * lifted into the shell and re-taught for every new view. Reads only — a spec
 * build is a mutation, something the reader asked for and is already watching.
 *
 * It is placed before the right-hand group rather than in it. That group is held
 * against the right edge by `margin-left:auto`, so an item arriving inside would
 * slide the stamp and the search box sideways every time the corpus was read;
 * here it grows into the space between the links and them, and nothing already
 * placed moves.
 */
export function NavReading() {
  const reading = useIsFetching();
  if (reading === 0) return null;
  return (
    <span className="reading" role="status">
      Reading the corpus…
    </span>
  );
}
