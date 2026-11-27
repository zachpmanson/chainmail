import TextArea from "../ui/TextArea";

export default function RestoredReplyQuote({
  quote,
  after,
  showAfter,
  disabled,
  onAfter,
}: {
  quote: string;
  after: string;
  showAfter: boolean;
  disabled: boolean;
  onAfter: (value: string) => void;
}) {
  return (
    <>
      <details className="mt-2 text-sm text-muted">
        <summary className="cursor-pointer">Quoted history</summary>
        <blockquote className="my-2 border-l-2 border-line pl-3 wrap-anywhere whitespace-pre-wrap">
          {quote}
        </blockquote>
      </details>
      {showAfter ? (
        <label className="mt-2 block text-xs text-muted">
          Reply below quoted history
          <TextArea
            className="mt-1 w-full"
            rows={2}
            value={after}
            disabled={disabled}
            onChange={(e) => onAfter(e.target.value)}
          />
        </label>
      ) : null}
    </>
  );
}
