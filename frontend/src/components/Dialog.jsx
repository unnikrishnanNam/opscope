import { useEffect, useId, useRef } from "react";
import Button from "./Button.jsx";
import { Callout } from "./Callout.jsx";
import { CloseIcon } from "./icons.jsx";
import "./Dialog.css";

// Dialog and Drawer are built on the browser's <dialog> element, which
// keeps keyboard focus inside while open, makes the page behind it inert,
// closes on Escape and puts focus back where it was afterwards.
//
// Both are controlled: the parent owns `open` and closes it in `onClose`
// (called for Escape, the close button, and a click on the backdrop).

function useModal(open, onClose) {
  const ref = useRef(null);
  const pressedBackdrop = useRef(false);

  useEffect(() => {
    const dialog = ref.current;
    if (open && !dialog.open) {
      dialog.showModal();
      // React doesn't pass `autofocus` to the page, so mark the element that
      // should get focus with data-autofocus instead (e.g. Cancel).
      dialog.querySelector("[data-autofocus]")?.focus();
    }
    if (!open && dialog.open) dialog.close();
  }, [open]);

  return {
    ref,
    // Escape: leave it to the parent, which owns `open`.
    onCancel: (event) => {
      event.preventDefault();
      onClose();
    },
    // A click on the backdrop lands on the <dialog> itself, outside its
    // content. It only counts when the press started there too, so dragging
    // a text selection out of the dialog doesn't close it.
    onPointerDown: (event) => {
      pressedBackdrop.current = event.target === event.currentTarget;
    },
    onClick: (event) => {
      if (pressedBackdrop.current && event.target === event.currentTarget) onClose();
    },
    // The browser can still close it by itself (Chrome does on a second
    // Escape); tell the parent, so `open` doesn't stay true for a closed dialog.
    onClose: () => {
      if (open) onClose();
    },
  };
}

// Dialog: a box in the middle of the screen.
//   title    heading (also names it for screen readers)
//   actions  buttons along the bottom, the main one last
export function Dialog({ open, onClose, title, actions, children }) {
  const titleId = useId();
  const modal = useModal(open, onClose);
  return (
    <dialog className="dialog" aria-labelledby={titleId} {...modal}>
      <div className="dialog-content">
        <header className="dialog-header">
          <h2 id={titleId} className="dialog-title">
            {title}
          </h2>
          <Button variant="quiet" size="sm" icon={CloseIcon} label="Close" onClick={onClose} />
        </header>
        <div className="dialog-body">{children}</div>
        {actions && <footer className="dialog-actions">{actions}</footer>}
      </div>
    </dialog>
  );
}

// ConfirmDialog: "are you sure?" before something that can't be undone.
// Focus starts on Cancel, so pressing Enter by habit doesn't confirm.
//   confirmLabel  says what happens, e.g. "Remove cluster"
//   busy          shows progress on the confirm button while it runs
//   error         an Error to show if it failed (the dialog stays open)
export function ConfirmDialog({ open, onCancel, onConfirm, title, confirmLabel, busy = false, error, children }) {
  return (
    <Dialog
      open={open}
      onClose={busy ? () => {} : onCancel}
      title={title}
      actions={
        <>
          <Button onClick={onCancel} disabled={busy} data-autofocus>
            Cancel
          </Button>
          <Button variant="danger" onClick={onConfirm} loading={busy}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {children}
      {error && (
        <div className="dialog-error">
          <Callout tone="error" title="That didn't work" detail={error.detail}>
            {error.message}
          </Callout>
        </div>
      )}
    </Dialog>
  );
}

// Drawer: a panel that slides in from the left, for the sidebar on small
// screens. `label` names it for screen readers ("Navigation").
export function Drawer({ open, onClose, label, children }) {
  const modal = useModal(open, onClose);
  return (
    <dialog className="drawer" aria-label={label} {...modal}>
      <div className="drawer-content">{children}</div>
    </dialog>
  );
}
