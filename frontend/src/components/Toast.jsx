import { dismissToast, useToast } from "../toast.js";
import { CheckIcon, CloseIcon, WarningIcon } from "./icons.jsx";
import "./Toast.css";

// Toaster: where toasts appear (see toast.js), at the bottom of the window.
// Put one in the layout. The region is always there, even empty, so screen
// readers announce each message as it arrives.
export default function Toaster() {
  const current = useToast();
  return (
    <div className="toaster" role="status" aria-live="polite">
      {/* key: a new toast starts its animation again, even with the same text. */}
      {current && <ToastView key={current.id} {...current} onDismiss={dismissToast} />}
    </div>
  );
}

// ToastView: one toast's look, without the queue (also used on /kit).
//   tone  "neutral" (a tick) or "error" (a warning sign)
export function ToastView({ message, tone = "neutral", onDismiss }) {
  const Icon = tone === "error" ? WarningIcon : CheckIcon;
  return (
    <div className={`toast toast-${tone}`}>
      <Icon size={16} className="toast-icon" />
      <span className="toast-message">{message}</span>
      <button type="button" className="toast-close" aria-label="Dismiss" onClick={onDismiss}>
        <CloseIcon size={14} />
      </button>
    </div>
  );
}
