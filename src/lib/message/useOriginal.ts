import { useEffect, useRef, useState } from "react";
import { usePrefs, type ReadingPreference } from "../prefs/usePrefs";
import type { OriginalAnalysis, OriginalPayload } from "./original";

/** `asking` waits for a rendering decision; `none` means no original is available. */
export type Original =
  { at: "read" } | { at: "asking" } | { at: "sent"; html: string } | { at: "none"; why: string };

type Source = { extId: string; load: (extId: string) => Promise<OriginalPayload> };
const read: Original = { at: "read" };
const asking: Original = { at: "asking" };

/** The display choice is per sender; fetched content belongs to one message and loader. */
export function useOriginal(
  original: Source | undefined,
  fromEmail: string | undefined,
  person: { id: number; preferOriginal: ReadingPreference } | undefined,
  onPreferOriginal: ((next: ReadingPreference) => Promise<void>) | undefined,
  explain = false,
) {
  const key = fromEmail || original?.extId || "";
  const extId = original?.extId;
  const load = original?.load;
  const stored = person !== undefined && onPreferOriginal !== undefined;
  const styled = usePrefs((s) => key !== "" && s.styledSenders.includes(key));
  const plain = usePrefs((s) => key !== "" && s.plainSenders.includes(key));
  const setStyled = usePrefs((s) => s.setStyled);
  const preference: ReadingPreference = stored
    ? person.preferOriginal
    : styled
      ? true
      : plain
        ? false
        : undefined;
  const explicit = preference !== undefined;
  const [result, setResult] = useState<
    (Source & { state: Original; analysis?: OriginalAnalysis }) | null
  >(null);
  const arrived = useRef<(Source & OriginalPayload) | null>(null);
  const current = result && result.extId === extId && result.load === load ? result : null;
  const on = preference ?? current?.analysis?.defaultsToOriginal === true;

  useEffect(() => {
    if (!extId || !load || (preference === false && !explain)) return;
    let live = true;
    const show = (html: string, analysis: OriginalAnalysis | undefined) => {
      if (!live) return;
      arrived.current = { extId, load, html, analysis };
      setResult({ extId, load, analysis, state: { at: "sent", html } });
    };
    if (arrived.current?.extId === extId && arrived.current.load === load) {
      show(arrived.current.html, arrived.current.analysis);
    } else {
      setResult({ extId, load, state: { at: "asking" } });
      load(extId).then(
        ({ html, analysis }) => {
          if (live) show(html, analysis);
        },
        (err: unknown) => {
          if (!live) return;
          setResult({
            extId,
            load,
            state: {
              at: "none",
              why:
                err instanceof Error && err.message ? err.message : "the original is not available",
            },
          });
        },
      );
    }
    return () => {
      live = false;
    };
  }, [preference, extId, load, explain]);

  const choose = (next: ReadingPreference) => {
    if (stored) {
      void onPreferOriginal(next).then(
        () => {
          if (next === undefined) setStyled(key, undefined);
        },
        // The server hook restores the saved choice and reports the error.
        () => {},
      );
    } else {
      setStyled(key, next);
    }
  };
  // Even a cached promise resolves after the first render; don't paint a provisional body.
  const pending =
    extId && load && preference !== false && (!current || current.state.at === "asking");
  const state = pending ? asking : on && current ? current.state : read;
  return {
    on,
    state,
    preference,
    choose,
    explicit,
    analysis: current?.analysis,
    analysisState: current?.state.at === "sent" && !on ? "read" : current?.state.at,
  };
}
