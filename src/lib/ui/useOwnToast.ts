import { useEffect, useRef } from "react";
import { dismissToast, pushToast, SAID_MS, type Toast } from "./toasts";

/** One toast per caller: each `say` replaces the last, and a new `resetKey` takes it down. */
export default function useOwnToast(resetKey?: unknown) {
  const said = useRef<number | null>(null);
  useEffect(() => {
    if (said.current !== null) dismissToast(said.current);
    said.current = null;
  }, [resetKey]);
  return (text: string, kind: Toast["kind"] = "note") => {
    if (said.current !== null) dismissToast(said.current);
    said.current = pushToast(text, kind, kind === "note" ? SAID_MS : null);
  };
}
