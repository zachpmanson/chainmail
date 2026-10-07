import { $api } from "../../lib/api/api";
import { isoDay, when } from "../../lib/ui/stamp";

/** Asks the server: the bundle is identical across deploys of a revision, so it can't tell. */
export default function DeployStamp() {
  const version = $api.useQuery("get", "/v1/version", {});
  const rev = version.data?.rev;
  const startedAt = version.data?.startedAt;
  if (!rev || !startedAt) return null;

  const at = new Date(startedAt);
  const on = Number.isNaN(at.getTime()) ? startedAt : isoDay(at);

  return (
    <a
      className="flex items-baseline gap-1.5 whitespace-nowrap text-[.74rem] text-muted no-underline hover:text-accent"
      href={`https://github.com/zachpmanson/chainmail/commit/${rev}`}
      title={`deployed ${when(startedAt)}`}
      target="_blank"
      rel="noreferrer"
    >
      <span>{on}</span> @ <code className="font-mono text-[.72rem]">{rev.slice(0, 7)}</code>
    </a>
  );
}
