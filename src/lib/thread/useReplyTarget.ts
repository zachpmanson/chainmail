import { useEffect, useState } from "react";
import type { CorpusEntry } from "../api/api";
import { newest } from "../inbox/newest";
import { gmailIdOf } from "../message/sources";

/** Which message the reply box answers, and whether it answers all. */
export function useReplyTarget(
  threadId: string,
  shown: CorpusEntry[],
): {
  answer: CorpusEntry | undefined;
  all: boolean;
  setAll: (all: boolean) => void;
  aimed: number;
  aim: (extId: string) => void;
} {
  // Kept as an ext id because entries are re-read after a send; an id that no longer
  // resolves falls back to the default.
  const [answering, setAnswering] = useState<string | null>(null);
  const [all, setAll] = useState(true);
  // A count, not a flag: re-pressing on the already-answered message must still register.
  const [aimed, setAimed] = useState(0);

  // Reset the reply target on thread change without remounting, so the draft survives.
  useEffect(() => {
    setAnswering(null);
    setAll(true);
  }, [threadId]);

  // Newest, not last drawn (tree view reorders), and only entries the mailbox holds:
  // quote-recovered entries can't be threaded onto, and the server would refuse them.
  const newestAnswer = newest(shown.filter((e) => gmailIdOf(e) !== undefined));
  // Re-check answerability: re-reads can drop an entry's mailbox copy.
  const chosen = shown.find((e) => e.extId === answering && gmailIdOf(e) !== undefined);

  const aim = (extId: string) => {
    setAnswering(extId);
    setAll(true);
    setAimed((n) => n + 1);
  };

  return { answer: chosen ?? newestAnswer, all, setAll, aimed, aim };
}
