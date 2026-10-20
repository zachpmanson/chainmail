import { useEffect, useRef, useState } from "react";
import { usePrefs } from "../prefs/usePrefs";
import { defaultsToOriginal } from "./originalDefault";

/** `asking` keeps the transcript's body on screen; `none` means no original is available. */
export type Original =
  { at: "read" } | { at: "asking" } | { at: "sent"; html: string } | { at: "none"; why: string };

type Source = { extId: string; load: (extId: string) => Promise<string> };
const read: Original = { at: "read" };

/** The display choice is per sender; fetched content belongs to one message and loader. */
export function useOriginal(
  original: Source | undefined,
  fromEmail: string | undefined,
  person: { id: number; preferOriginal: boolean | undefined } | undefined,
  onPreferOriginal: ((next: boolean) => void) | undefined,
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
  const [result, setResult] = useState<(Source & { state: Original }) | null>(null);
  const arrived = useRef<(Source & { html: string }) | null>(null);

  useEffect(() => {
    if (!extId || !load || (explicit && !on)) return;
    let live = true;
    const show = (html: string) => {
      if (!live) return;
      arrived.current = { extId, load, html };
      const selected = explicit ? on : defaultsToOriginal(html);
      setAutomatic({ extId, load, on: selected });
      setResult({ extId, load, state: selected ? { at: "sent", html } : read });
    };
    if (arrived.current?.extId === extId && arrived.current.load === load) {
      show(arrived.current.html);
    } else {
      setResult({ extId, load, state: { at: "asking" } });
      load(extId).then(show, (err: unknown) => {
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
      });
    }
    return () => {
      live = false;
    };
  }, [on, explicit, extId, load]);

  const ask = () => (stored ? onPreferOriginal(!on) : setStyled(key, !on));
  const state =
    on && result && result.extId === extId && result.load === load ? result.state : read;
  return { on, state, ask };
}
