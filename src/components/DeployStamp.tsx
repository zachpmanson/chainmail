import { $api } from "../lib/api";
import { isoDay, when } from "../lib/stamp";

/**
 * What is running, in the nav, immediately left of the search box.
 *
 * The question this answers is "is the fix I just merged live", and nothing baked
 * into the bundle can answer it: the bundle is byte-identical across deploys of
 * one revision, so a stamp built into it reads the same on the code that is
 * running and on the copy a tab has been holding since before the fix. So the
 * server is asked — it knows the revision it was started with and when it came up.
 *
 * Two facts, because neither answers alone: a hash with no date cannot say
 * whether it is a minute old or a week old, and a date with no hash cannot say
 * what is running. The date is the *process's* start, so a changed date with an
 * unchanged hash is a restart rather than a deploy — which is worth being able to
 * tell, and is why the tooltip names the time as well as the day.
 *
 * The revision links to the commit, as penultimate-guitar's footer stamp does:
 * the stamp is the entry point to "what changed", and a hash is a search away
 * from being nothing.
 *
 * Nothing is rendered when the server reports no revision, or before it answers.
 * A build nobody labelled shows no stamp rather than a placeholder — a nav that
 * says "loading…" in the corner where a commit hash belongs is noise on every
 * page whose question nobody asked. A dev server under the devshell is exactly
 * that case.
 */
export function DeployStamp() {
  const version = $api.useQuery("get", "/v1/version", {});
  const rev = version.data?.rev;
  const startedAt = version.data?.startedAt;
  if (!rev || !startedAt) return null;

  const at = new Date(startedAt);
  const on = Number.isNaN(at.getTime()) ? startedAt : isoDay(at);

  return (
    <a
      className="deploy"
      href={`https://github.com/zachpmanson/chainmail/commit/${rev}`}
      title={`deployed ${when(startedAt)}`}
      target="_blank"
      rel="noreferrer"
    >
      {/* A hash is read character by character only when something looks wrong,
          so it is monospaced and quiet: a fainter face beside the links. */}
      <code>{rev.slice(0, 7)}</code> <span>{on}</span>
    </a>
  );
}
