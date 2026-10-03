import { useState } from "react";

// `cm-list` already stores the inbox's resizable panel width (lib/panelWidth.ts),
// so the compact preference needs its own key.
const KEY = "cm-compact";

export function readCompactMode(): boolean {
  try {
    return localStorage.getItem(KEY) === "1";
  } catch {
    return false;
  }
}

export function useCompactMode(): [boolean, (compact: boolean) => void] {
  const [compact, setCompact] = useState(readCompactMode);
  const set = (value: boolean) => {
    setCompact(value);
    try {
      localStorage.setItem(KEY, value ? "1" : "0");
    } catch {
      // Storage may be unavailable (e.g. private browsing); the in-session toggle still works.
    }
  };
  return [compact, set];
}
