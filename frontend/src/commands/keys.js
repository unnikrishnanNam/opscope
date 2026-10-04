import { useEffect, useRef } from "react";

// Keyboard shortcuts for commands. A command's `shortcut` is one key ("/",
// "2", "?") or a sequence of keys pressed one after another ("g p": g, then
// p). The same registry that fills the palette binds them, so a shortcut
// shown in the palette or the help always works, and the other way round.
//
// Shortcuts are plain keys, so they're ignored while typing in a field and
// while a dialog is open (including the palette itself); ⌘K is the one
// modifier shortcut, and the layout handles it.

// How long the second key of a sequence may wait.
const SEQUENCE_MS = 1000;

export function keysOf(shortcut) {
  return shortcut.split(" ");
}

// matchShortcut decides what a key press does, given the keys pressed so far:
//   { command }   it completes this command's shortcut: run it
//   { pending }   it starts (or continues) a sequence: wait for the next key
//   null          nothing; forget any sequence
// When two commands share a shortcut, the first wins (page commands come
// first, so a page can take a key from a global command).
export function matchShortcut(commands, pending, key) {
  const pressed = [...pending, key];
  const command = commands.find((c) => sameKeys(keysOf(c.shortcut), pressed));
  if (command) return { command };
  if (commands.some((c) => startsWith(keysOf(c.shortcut), pressed))) return { pending: pressed };
  // "g" then "/": the sequence is broken, but "/" may be a shortcut by itself.
  return pending.length ? matchShortcut(commands, [], key) : null;
}

function sameKeys(a, b) {
  return a.length === b.length && a.every((k, i) => k === b[i]);
}

function startsWith(keys, prefix) {
  return keys.length > prefix.length && prefix.every((k, i) => k === keys[i]);
}

// isTyping: is this key press going into a field?
function isTyping(target) {
  return Boolean(target.closest?.("input, textarea, select, [contenteditable]:not([contenteditable='false'])"));
}

// useShortcuts listens for the shortcuts of `commands` (those with a
// `shortcut`) and calls run(command) when one is pressed.
export function useShortcuts(commands, run) {
  // The listener is added once; it reads the latest commands from here.
  const latest = useRef({ commands, run });
  latest.current = { commands, run };

  useEffect(() => {
    let pending = [];
    let timer = null;

    function onKeyDown(event) {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) return;
      if (isTyping(event.target) || document.querySelector("dialog[open]")) {
        pending = [];
        return;
      }
      const result = matchShortcut(latest.current.commands, pending, event.key);
      clearTimeout(timer);
      pending = result?.pending ?? [];
      if (!result) return;
      event.preventDefault();
      if (result.command) latest.current.run(result.command);
      else timer = setTimeout(() => (pending = []), SEQUENCE_MS);
    }

    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      clearTimeout(timer);
    };
  }, []);
}
