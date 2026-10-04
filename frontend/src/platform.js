// Which modifier key this computer uses for shortcuts like ⌘K: Command on
// Apple devices, Ctrl elsewhere. Ctrl+K on a Mac stays the text fields'
// "delete to the end of the line".

export const isApple = /Mac|iPhone|iPad/.test(navigator.platform);

// The key as its keyboard labels it, for hints: "⌘" or "Ctrl".
export const modKey = isApple ? "⌘" : "Ctrl";

// hasModKey(event): was this computer's modifier held (and not the other one)?
export function hasModKey(event) {
  return isApple ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
}
