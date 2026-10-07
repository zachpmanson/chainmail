import { useEffect, useRef } from "react";
import { mountOriginal } from "../../lib/message/original";
import type { Original } from "../../lib/message/useOriginal";
import { hasBody, trimBody } from "../../lib/message/trimBody";

/** Renderings are keyed apart: a shadow root can't be detached, so a reused div would keep drawing the sender's HTML. */
export default function Body({ body, state }: { body: string; state: Original }) {
  const host = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (state.at === "sent" && host.current) mountOriginal(host.current, state.html);
  }, [state]);

  if (state.at === "sent") {
    // `contain` keeps the sender's position:fixed and fixed-width markup inside the pane.
    return (
      <div key="sent" className="bd overflow-x-auto contain-layout contain-paint" ref={host} />
    );
  }
  if (!hasBody(body)) {
    return (
      <div key="read" className="bd overflow-x-auto">
        <p className="m-0 text-sm italic text-muted">No body</p>
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
