import type { ComposeResponse } from "../../lib/api/api";
import Button from "../ui/Button";

export default function ComposeDone({
  result,
  onClose,
}: {
  result: ComposeResponse;
  onClose: () => void;
}) {
  return (
    <div role="status">
      <p>
        Sent to {result.to}
        {result.cc ? `, cc ${result.cc}` : ""}.
      </p>
      <p>
        {result.filed
          ? "Filed in the corpus."
          : "Sent successfully, but could not be filed in the corpus."}
      </p>
      <Button variant="subtle" density="compact" type="button" onClick={onClose}>
        Done
      </Button>
    </div>
  );
}
