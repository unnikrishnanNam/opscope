import { useSyncExternalStore } from "react";

// Toasts: short confirmations at the bottom of the window, like "Copied
// kubectl command", for actions whose result isn't visible on the page
// (the command palette has closed by then). Anything can show one:
//
//   toast("Copied the name");
//   toast("Couldn't copy: the browser didn't allow it", { tone: "error" });
//
// One shows at a time; a new one replaces the last. <Toaster> (in the
// layout) draws it.

const SHOW_MS = 3000;

let current = null; // { id, message, tone }
let timer = null;
let nextId = 1;
const listeners = new Set();

function set(value) {
  current = value;
  listeners.forEach((notify) => notify());
}

export function toast(message, { tone = "neutral" } = {}) {
  clearTimeout(timer);
  set({ id: nextId++, message, tone });
  timer = setTimeout(() => set(null), SHOW_MS);
}

export function dismissToast() {
  clearTimeout(timer);
  set(null);
}

function subscribe(notify) {
  listeners.add(notify);
  return () => listeners.delete(notify);
}

// useToast returns the toast on screen now ({ id, message, tone }), or null.
export function useToast() {
  return useSyncExternalStore(subscribe, () => current);
}
