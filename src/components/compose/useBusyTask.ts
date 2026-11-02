import { useRef, useState } from "react";

/** One request at a time, with its failure kept as words for the box to show. */
export default function useBusyTask() {
  const operationRevision = useRef(0);
  const busyRef = useRef(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function run(task: () => Promise<void>, describe: (e: unknown) => string) {
    operationRevision.current++;
    busyRef.current = true;
    setError(null);
    setBusy(true);
    try {
      await task();
    } catch (e) {
      setError(describe(e));
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }

  return { busy, busyRef, operationRevision, error, clearError: () => setError(null), run };
}
