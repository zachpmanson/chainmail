import { useState } from "react";

/** One request at a time, with its failure kept as words for the box to show. */
export default function useBusyTask() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function run(task: () => Promise<void>, describe: (e: unknown) => string) {
    setError(null);
    setBusy(true);
    try {
      await task();
    } catch (e) {
      setError(describe(e));
    } finally {
      setBusy(false);
    }
  }

  return { busy, error, clearError: () => setError(null), run };
}
