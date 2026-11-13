import { useState } from "react";
import Button from "../ui/Button";
import InlineAlert from "../ui/InlineAlert";
import TextInput from "../ui/TextInput";

export default function HassleTitleForm({
  title,
  busy,
  onSave,
  onClose,
}: {
  title: string;
  busy: boolean;
  onSave: (title: string) => void;
  onClose: () => void;
}) {
  const [value, setValue] = useState(title);
  const [error, setError] = useState("");
  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(event) => {
        event.preventDefault();
        if (busy) return;
        const clean = value.trim();
        if (!clean || /[\r\n]/.test(clean) || new TextEncoder().encode(clean).length > 500) {
          setError("Enter a one-line title (maximum 500 bytes).");
          return;
        }
        onSave(clean);
      }}
    >
      <h2 className="m-0 text-lg">Edit title</h2>
      <TextInput
        aria-label="Title"
        value={value}
        disabled={busy}
        required
        onChange={(event) => setValue(event.target.value)}
      />
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      <div className="flex justify-end gap-2">
        <Button type="button" disabled={busy} onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" variant="primary" disabled={busy}>
          Save title
        </Button>
      </div>
    </form>
  );
}
