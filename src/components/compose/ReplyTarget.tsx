export default function ReplyTarget({
  answerAnchor,
  words,
}: {
  answerAnchor: string;
  words: { who: string; whoTitle?: string; when: string };
}) {
  return (
    <a
      className="par ml-auto inline-flex h-7 items-center gap-1 whitespace-nowrap text-2xs text-muted no-underline hover:text-accent"
      href={`#${answerAnchor}`}
      aria-label={`Jump to the message being replied to: ${words.who || "the sender"}, ${words.when}`}
      title={`Jump to the message being replied to: ${words.whoTitle ?? words.who}, ${words.when}`}
    >
      <span className="block translate-y-[.045em] text-sm leading-none" aria-hidden="true">
        &#8617;
      </span>
      <span>{words.who || "message"}</span>
    </a>
  );
}
