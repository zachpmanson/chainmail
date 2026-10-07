import type { Draft } from "./RuleRow";

export default function Consequence({
  shift,
  draft,
}: {
  shift: { messages: number; people: number; ambiguous: number };
  draft: Draft;
}) {
  const { messages, people, ambiguous } = shift;
  return (
    <p>
      {draft.org === null ? (
        <>
          <strong>{draft.domain}</strong> goes back to being read from its own name.
        </>
      ) : draft.org === "" ? (
        <>
          <strong>{draft.domain}</strong> is not an organisation — its mail leaves whatever grouping
          it is in and takes the unknown colour.
        </>
      ) : (
        <>
          <strong>{draft.domain}</strong> joins every other domain drawn as{" "}
          <strong>{draft.org}</strong>.
        </>
      )}{" "}
      {messages === 0
        ? "No message changes colour."
        : `${messages} message${messages === 1 ? "" : "s"} from ${people} sender${
            people === 1 ? "" : "s"
          } would be drawn differently.`}
      {ambiguous > 0
        ? ` ${ambiguous} more cannot be placed at all: ${
            ambiguous === 1 ? "its sender's" : "their senders'"
          } own mail names two organisations, and the entry has no address of its own.`
        : null}
    </p>
  );
}
