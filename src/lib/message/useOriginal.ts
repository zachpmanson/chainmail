import { useEffect, useRef, useState } from "react";
import { usePrefs } from "../prefs/usePrefs";

/** `asking` keeps the transcript's body on screen; `none` is the corpus saying this message has no original part. */
export type Original =
  { at: "read" } | { at: "asking" } | { at: "sent"; html: string } | { at: "none"; why: string };

/**
 * The sender's own HTML, swapped in for the transcript's rendering. The switch is per
 * sender: stored on the person where there is a corpus, else in the browser's prefs (lib/prefs/usePrefs).
 */
export function useOriginal(
  original: { extId: string; load: (extId: string) => Promise<string> } | undefined,
  fromEmail: string | undefined,
  person: { id: number; preferOriginal: boolean } | undefined,
  onPreferOriginal: ((next: boolean) => void) | undefined,
) {
  // A recovered entry has no From header, so the local switch falls back to the message id.
  const key = fromEmail || original?.extId || "";
  const extId = original?.extId;
  const load = original?.load;
  const stored = person !== undefined && onPreferOriginal !== undefined;

  // Locally, one sender's bubbles share the switch, so each follows it rather than owning it.
  const styled = usePrefs((s) => key !== "" && s.styledSenders.includes(key));
  const toggleStyled = usePrefs((s) => s.toggleStyled);
  const on = stored ? person.preferOriginal : styled;
  const [state, setState] = useState<Original>({ at: "read" });
  const arrived = useRef<string | null>(null);

  useEffect(() => {
    if (!extId || !load) return;
    if (!on) {
      // Same object when unchanged, so switching off doesn't re-render every bubble already off.
      setState((s) => (s.at === "read" ? s : { at: "read" }));
      return;
    }
    if (arrived.current !== null) {
      setState({ at: "sent", html: arrived.current });
      return;
    }
    // A fetch landing after the switch went off must not swap the body back.
    let live = true;
    setState({ at: "asking" });
    load(extId).then(
      (html) => {
        arrived.current = html;
        if (live) setState({ at: "sent", html });
      },
      (err: unknown) => {
        if (live) {
          setState({
            at: "none",
            why:
              err instanceof Error && err.message ? err.message : "the original is not available",
          });
        }
      },
    );
    return () => {
      live = false;
    };
  }, [on, extId, load]);

  const ask = () => (stored ? onPreferOriginal(!on) : toggleStyled(key));

  return { on, state, ask };
}
