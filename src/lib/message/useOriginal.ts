import { useEffect, useRef, useState } from "react";
import { usePrefs } from "../prefs/usePrefs";
import type { OriginalAnalysis, OriginalPayload } from "./original";

/** `asking` keeps the transcript's body on screen; `none` means no original is available. */
export type Original =
  { at: "read" } | { at: "asking" } | { at: "sent"; html: string } | { at: "none"; why: string };

type Source = { extId: string; load: (extId: string) => Promise<OriginalPayload> };
const read: Original = { at: "read" };

/** The display choice is per sender; fetched content belongs to one message and loader. */
export function useOriginal(
  original: Source | undefined,
  fromEmail: string | undefined,
  person: { id: number; preferOriginal: boolean | undefined } | undefined,
  onPreferOriginal: ((next: boolean) => void) | undefined,
  explain = false,
) {
  const key = fromEmail || original?.extId || "";
  const extId = original?.extId;
  const load = original?.load;
  const stored = person !== undefined && onPreferOriginal !== undefined;
  const styled = usePrefs((s) => key !== "" && s.styledSenders.includes(key));
  const plain = usePrefs((s) => key !== "" && s.plainSenders.includes(key));
  const setStyled = usePrefs((s) => s.setStyled);
  const explicit = stored ? person.preferOriginal !== undefined : styled || plain;
  const [automatic, setAutomatic] = useState<(Source & { on: boolean }) | null>(null);
  const on = explicit
    ? stored
      ? person.preferOriginal === true
      : styled
    : Boolean(automatic && automatic.extId === extId && automatic.load === load && automatic.on);
  const [result, setResult] = useState<
    (Source & { state: Original; analysis?: OriginalAnalysis }) | null
  >(null);
  const arrived = useRef<(Source & OriginalPayload) | null>(null);

  useEffect(() => {
    if (!extId || !load || (explicit && !on && !explain)) return;
    let live = true;
    const show = (html: string, analysis: OriginalAnalysis | undefined) => {
      if (!live) return;
      arrived.current = { extId, load, html, analysis };
      const selected = explicit ? on : analysis?.defaultsToOriginal === true;
      setAutomatic({ extId, load, on: selected });
      setResult({ extId, load, analysis, state: selected ? { at: "sent", html } : read });
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
          setAutomatic({ extId, load, on: false });
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
  }, [on, explicit, extId, load, explain]);

  const ask = () => (stored ? onPreferOriginal(!on) : setStyled(key, !on));
  const current = result && result.extId === extId && result.load === load ? result : null;
  const state = on && current ? current.state : read;
  return {
    on,
    state,
    ask,
    explicit,
    analysis: current?.analysis,
    analysisState: current?.state.at,
  };
}
