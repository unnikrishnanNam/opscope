import { useLayoutEffect, useRef, useState } from "react";
import { ArrowDownToLineIcon } from "./icons.jsx";
import { Spinner } from "./Loading.jsx";
import "./LogView.css";

// How close to the bottom (px) still counts as "at the bottom".
const BOTTOM_SLACK = 24;

// LogView: the box that shows log text. The controls (container, lines,
// follow) live outside it, in the page's toolbar.
//   text     the log lines so far
//   loading  true until the first bytes arrive
//   live     true while following: shows a "Live" marker
//   wrap     wrap long lines (default) or scroll sideways
//
// It opens at the newest line and stays there as lines arrive, unless
// you've scrolled up to read; then "Jump to newest" brings you back.
export default function LogView({ text, loading = false, live = false, wrap = true, height = "60vh" }) {
  const boxRef = useRef(null);
  const atBottom = useRef(true);
  const [showJump, setShowJump] = useState(false);

  // After new text is drawn, keep the newest line in view if we were there.
  useLayoutEffect(() => {
    const box = boxRef.current;
    if (box && atBottom.current) box.scrollTop = box.scrollHeight;
  }, [text]);

  function onScroll() {
    const box = boxRef.current;
    atBottom.current = box.scrollHeight - box.scrollTop - box.clientHeight < BOTTOM_SLACK;
    setShowJump(!atBottom.current);
  }

  function jumpToNewest() {
    const box = boxRef.current;
    box.scrollTo({ top: box.scrollHeight, behavior: "smooth" });
  }

  return (
    <div className="log-view" style={{ height }}>
      {live && (
        <span className="log-view-live">
          <span className="log-view-live-dot" aria-hidden="true" />
          Live
        </span>
      )}
      <pre
        ref={boxRef}
        className={["log-view-text", wrap && "is-wrapped", live && "is-live"].filter(Boolean).join(" ")}
        onScroll={onScroll}
        tabIndex={0} // so it can be scrolled with the keyboard
        aria-label="Log output"
        aria-busy={loading || undefined}
      >
        {text ||
          (loading ? (
            <span className="log-view-note">
              <Spinner size={14} /> Loading logs…
            </span>
          ) : (
            <span className="log-view-note">No log lines.</span>
          ))}
      </pre>
      {showJump && text && (
        <button type="button" className="log-view-jump" onClick={jumpToNewest}>
          <ArrowDownToLineIcon size={14} />
          Jump to newest
        </button>
      )}
    </div>
  );
}
