import { IconArrowBackUp } from "@tabler/icons-react";

export default function ReplyTarget({
  answerAnchor,
  words,
}: {
  answerAnchor: string;
  words: { who: string; whoTitle?: string; when: string };
}) {
  return (
    <a
      className="par ml-auto inline-flex h-7 items-center gap-1 text-2xs whitespace-nowrap text-muted no-underline hover:text-accent"
      href={`#${answerAnchor}`}
      aria-label={`Jump to the message being replied to: ${words.who || "the sender"}, ${words.when}`}
      title={`Jump to the message being replied to: ${words.whoTitle ?? words.who}, ${words.when}`}
    >
      <IconArrowBackUp className="block size-3.5 shrink-0" aria-hidden="true" />
      <span>{words.who || "message"}</span>
    </a>
  );
}
