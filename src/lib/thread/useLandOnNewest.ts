import { useEffect, useRef, useState } from "react";
import type { CorpusEntry } from "../api/api";
import { newest } from "../inbox/newest";
import { anchor } from "./lookup";

/** Lands on the newest entry once per thread; refetches must not move a reader who scrolled. */
export function useLandOnNewest(
  threadId: string,
  shown: CorpusEntry[],
): { landed: string | null; endLanding: () => void } {
  const [landed, setLanded] = useState<string | null>(null);
  const landedFor = useRef<string | null>(null);
  const target = newest(shown);
  useEffect(() => {
    if (!target || landedFor.current === threadId) return;
    landedFor.current = threadId;
    document.getElementById(anchor(shown.indexOf(target)))?.scrollIntoView({ block: "start" });
    setLanded(target.extId);
  }, [threadId, shown, target]);

  return { landed, endLanding: () => setLanded(null) };
}
