import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import "./Tooltip.css";

const HOVER_DELAY_MS = 350; // long enough not to flash while the mouse passes by
const GAP = 6; // px between the tooltip and what it points at

// Tooltip shows a short label above `children` on hover and on keyboard focus.
//
//   <Tooltip label="Refresh"><button aria-label="Refresh">…</button></Tooltip>
//
// It's for sighted mouse and keyboard users only (screen readers skip it), so
// whatever it wraps must already carry the words itself, e.g. an aria-label
// on an icon button, or the full text inside a cut-off cell.
//
// The label is drawn in a layer on top of the page (a portal), so a table
// that scrolls sideways can't clip it.
export default function Tooltip({ label, children }) {
  const triggerRef = useRef(null);
  const tipRef = useRef(null);
  const timer = useRef(null);
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState(null); // { top, left, below }

  function show(delay) {
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setOpen(true), delay);
  }

  function hide() {
    clearTimeout(timer.current);
    setOpen(false);
    setPosition(null);
  }

  // Place it above the trigger, or below when there's no room, and keep it
  // inside the window sideways. Runs before paint, so it never jumps.
  useLayoutEffect(() => {
    if (!open || !tipRef.current) return;
    const anchor = triggerRef.current.getBoundingClientRect();
    const tip = tipRef.current.getBoundingClientRect();
    const below = anchor.top - tip.height - GAP < 0;
    const top = below ? anchor.bottom + GAP : anchor.top - tip.height - GAP;
    const centre = anchor.left + anchor.width / 2 - tip.width / 2;
    const left = Math.min(Math.max(centre, GAP), window.innerWidth - tip.width - GAP);
    setPosition({ top, left, below });
  }, [open, label]);

  // While open: Escape closes it, and so does scrolling (it would drift away).
  useEffect(() => {
    if (!open) return;
    const onKey = (e) => e.key === "Escape" && hide();
    window.addEventListener("keydown", onKey);
    window.addEventListener("scroll", hide, true);
    window.addEventListener("resize", hide);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", hide, true);
      window.removeEventListener("resize", hide);
    };
  }, [open]);

  useEffect(() => () => clearTimeout(timer.current), []);

  if (!label) return children;

  return (
    <span
      ref={triggerRef}
      className="tooltip-trigger"
      onMouseEnter={() => show(HOVER_DELAY_MS)}
      onMouseLeave={hide}
      // React's focus events bubble, so these fire for the button inside.
      onFocus={(e) => e.target.matches(":focus-visible") && show(0)}
      onBlur={hide}
      onMouseDown={hide} // clicking means they've found it
    >
      {children}
      {open &&
        createPortal(
          <span
            ref={tipRef}
            className={`tooltip ${position?.below ? "tooltip-below" : ""}`}
            style={position ? { top: position.top, left: position.left } : { visibility: "hidden", top: 0, left: 0 }}
            aria-hidden="true"
          >
            {label}
          </span>,
          layerFor(triggerRef.current),
        )}
    </span>
  );
}

// The tooltip goes in the nearest element with its own theme (a panel on
// /kit), so it takes that theme's colours; normally that's just <body>.
function layerFor(trigger) {
  return trigger?.closest("[data-theme]:not(html)") ?? document.body;
}
