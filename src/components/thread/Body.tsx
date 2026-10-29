import { ArrowPathIcon } from "@heroicons/react/24/outline";
import { useEffect, useLayoutEffect, useRef } from "react";
import { mountOriginal } from "../../lib/message/original";
import type { Original } from "../../lib/message/useOriginal";
import { hasBody, trimBody } from "../../lib/message/trimBody";

// Static exports have no original loader and run no effects.
const useMountEffect = typeof document === "undefined" ? useEffect : useLayoutEffect;

/** Renderings are keyed apart: a shadow root can't be detached, so a reused div would keep drawing the sender's HTML. */
export default function Body({ body, state }: { body: string; state: Original }) {
  const host = useRef<HTMLDivElement | null>(null);
  useMountEffect(() => {
    if (state.at === "sent" && host.current) mountOriginal(host.current, state.html);
  }, [state]);

  if (state.at === "sent") {
    // `contain` keeps the sender's position:fixed and fixed-width markup inside the pane.
    return (
      <div key="sent" className="bd overflow-x-auto contain-layout contain-paint" ref={host} />
    );
  }
  if (state.at === "asking") {
    return (
      <div key="asking" className="bd text-sm text-muted" role="status">
        <ArrowPathIcon
          className="inline-block size-[1em] align-middle motion-safe:animate-spin"
          aria-hidden="true"
        />
        <span className="sr-only">Loading message…</span>
      </div>
    );
  }
  if (!hasBody(body)) {
    return (
      <div key="read" className="bd overflow-x-auto">
        <p className="m-0 text-sm text-muted italic">No body</p>
      </div>
    );
  }
  return (
    <div
      key="read"
      className="bd overflow-x-auto"
      dangerouslySetInnerHTML={{ __html: trimBody(body) }}
    />
  );
}
