import { ArrowUturnLeftIcon } from "@heroicons/react/24/outline";
import ReceiptIconButton from "../ui/ReceiptIconButton";

/** Also turns reply-all on. Drawn only when the message has a mailbox copy to
 *  thread onto (see gmailIdOf) and there is a box to point at it. */
export default function AnswerPress({
  extId,
  pressed,
  onPress,
}: {
  /** The message this press names, as the box takes it. */
  extId: string;
  /** Whether the box is already answering this message. */
  pressed: boolean;
  onPress: (extId: string) => void;
}) {
  const label = pressed
    ? "This is the message the box below is answering"
    : "Reply all to this message, in the box at the bottom of the thread";
  return (
    <ReceiptIconButton
      type="button"
      aria-pressed={pressed}
      title={label}
      aria-label={label}
      onClick={() => onPress(extId)}
    >
      <ArrowUturnLeftIcon width={18} height={18} aria-hidden="true" />
    </ReceiptIconButton>
  );
}
