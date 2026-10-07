import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

/**
 * Periodically refetches queries while the tab is visible. Never slurps: /v1/slurp holds a
 * one-at-a-time latch (409) and is built to run hourly, so a cadence would stack ingests.
 */

/** How often a visible page asks the corpus again. */
export const AUTO_REFRESH_MS = 90_000;

// Returning to a window fires both focus and visibilitychange; treat them as one wake.
const ONE_WAKE_MS = 1000;

export default function AutoRefresh() {
  const qc = useQueryClient();

  useEffect(() => {
    let ticker: ReturnType<typeof setInterval> | null = null;
    let reading = false;
    let lastWakeAt = -Infinity;

    // `cancelRefetch: false` leaves in-flight queries alone instead of restarting them.
    const read = async () => {
      if (reading || document.visibilityState === "hidden") return;
      reading = true;
      try {
        await qc.invalidateQueries({}, { cancelRefetch: false });
      } finally {
        reading = false;
      }
    };

    const wake = () => {
      const now = Date.now();
      if (now - lastWakeAt < ONE_WAKE_MS) return;
      lastWakeAt = now;
      void read();
    };

    const start = () => {
      if (ticker === null) ticker = setInterval(() => void read(), AUTO_REFRESH_MS);
    };
    const stop = () => {
      if (ticker !== null) {
        clearInterval(ticker);
        ticker = null;
      }
    };

    const onVisibility = () => {
      if (document.visibilityState === "hidden") {
        stop();
        return;
      }
      start();
      wake();
    };

    if (document.visibilityState !== "hidden") start();
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("focus", wake);

    return () => {
      stop();
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("focus", wake);
    };
  }, [qc]);

  return null;
}
