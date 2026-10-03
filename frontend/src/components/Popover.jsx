import { useEffect, useRef, useState } from "react";
import Button from "./Button.jsx";
import "./Popover.css";

// usePopover: open/closed state for anything that drops down from a button
// (menus, pickers). It closes on Escape, on a click outside, and when focus
// leaves it. Put `ref` on the element that wraps the button and the panel.
//
//   const popover = usePopover();
//   <div ref={popover.ref} className="popover-anchor">
//     <button onClick={popover.toggle}>…</button>
//     {popover.open && <div className="popover-panel">…</div>}
//   </div>
//
// `onClose` runs whenever it closes, e.g. to put focus back on the button.
export function usePopover({ onClose } = {}) {
  const ref = useRef(null);
  const [open, setOpen] = useState(false);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  function close({ returnFocus = false } = {}) {
    setOpen(false);
    onCloseRef.current?.({ returnFocus });
  }

  useEffect(() => {
    if (!open) return;
    function onPointerDown(event) {
      if (!ref.current?.contains(event.target)) close();
    }
    function onKeyDown(event) {
      if (event.key === "Escape") {
        event.stopPropagation();
        close({ returnFocus: true });
      }
    }
    function onFocusOut(event) {
      // relatedTarget is where focus went; null means it left the page.
      if (event.relatedTarget && !ref.current?.contains(event.relatedTarget)) close();
    }
    const el = ref.current;
    document.addEventListener("pointerdown", onPointerDown);
    el.addEventListener("keydown", onKeyDown);
    el.addEventListener("focusout", onFocusOut);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      el.removeEventListener("keydown", onKeyDown);
      el.removeEventListener("focusout", onFocusOut);
    };
  }, [open]);

  return { ref, open, setOpen, toggle: () => (open ? close() : setOpen(true)), close };
}

// Menu: a button that opens a short list of actions.
//
//   <Menu label="Cluster actions" icon={MenuIcon} items={[
//     { label: "Copy server address", icon: CopyIcon, onSelect: copy },
//     { label: "Remove", icon: TrashIcon, danger: true, onSelect: remove },
//   ]} />
//
// `label` names the button (and is its tooltip when icon-only); pass
// children to give it visible text instead. Arrow keys move through the
// items, Enter or Space picks one, Escape closes and returns to the button.
export function Menu({ label, icon, items, align = "start", variant = "quiet", size = "sm", children }) {
  const buttonRef = useRef(null);
  const listRef = useRef(null);
  const popover = usePopover({
    onClose: ({ returnFocus }) => returnFocus && buttonRef.current?.focus(),
  });

  // Opening moves focus to the first item.
  useEffect(() => {
    if (popover.open) listRef.current?.querySelector('[role="menuitem"]')?.focus();
  }, [popover.open]);

  function onKeyDown(event) {
    const entries = [...listRef.current.querySelectorAll('[role="menuitem"]')];
    const index = entries.indexOf(document.activeElement);
    const next = { ArrowDown: index + 1, ArrowUp: index - 1, Home: 0, End: entries.length - 1 }[event.key];
    if (next === undefined) return;
    event.preventDefault();
    entries[(next + entries.length) % entries.length].focus();
  }

  function pick(item) {
    popover.close({ returnFocus: true });
    item.onSelect();
  }

  return (
    <div ref={popover.ref} className="popover-anchor">
      <Button
        ref={buttonRef} // React 19 passes ref through like any prop; Button puts it on the <button>
        variant={variant}
        size={size}
        icon={icon}
        label={label}
        aria-haspopup="menu"
        aria-expanded={popover.open}
        onClick={popover.toggle}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" && !popover.open) {
            e.preventDefault();
            popover.setOpen(true);
          }
        }}
      >
        {children}
      </Button>
      {popover.open && (
        <ul
          ref={listRef}
          className={`popover-panel menu popover-${align}`}
          role="menu"
          aria-label={label}
          onKeyDown={onKeyDown}
        >
          {items.map((item) => {
            const Icon = item.icon;
            return (
              <li key={item.label} role="none">
                <button
                  type="button"
                  role="menuitem"
                  tabIndex={-1}
                  className={`menu-item ${item.danger ? "menu-item-danger" : ""}`}
                  onClick={() => pick(item)}
                >
                  {Icon && <Icon size={16} />}
                  {item.label}
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
