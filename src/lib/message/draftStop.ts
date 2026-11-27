export function confirmUncommittedRecipients(host: HTMLElement | null): boolean {
  const inputs = host?.querySelectorAll<HTMLInputElement>("[data-list] input[type=text]");
  return (
    !inputs ||
    !Array.from(inputs).some((input) => input.value.trim()) ||
    window.confirm("Stop editing? Recipient text you haven't added with Enter will not be saved.")
  );
}

export function canStopEditingDraft({
  generation,
  id,
  inputKey,
  acknowledgement,
  pending,
  failed,
}: {
  generation: number;
  id: string;
  inputKey: string;
  acknowledgement: string;
  pending: boolean;
  failed: boolean;
}): boolean {
  return (
    Boolean(id) &&
    !pending &&
    !failed &&
    acknowledgement === JSON.stringify([generation, id, inputKey])
  );
}
