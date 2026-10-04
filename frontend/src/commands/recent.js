// The commands used most recently, so the palette can offer them first.
// Kept in this browser only (localStorage), like the theme. Only ids are
// stored; a recent command shows up only while its source still offers it.

const STORAGE_KEY = "opscope-recent-commands";
const MAX = 10;

// getRecent() -> ["go.pods", "theme.dark", ...], most recent first
export function getRecent() {
  try {
    const saved = JSON.parse(localStorage.getItem(STORAGE_KEY));
    return Array.isArray(saved) ? saved.filter((id) => typeof id === "string").slice(0, MAX) : [];
  } catch {
    return []; // storage can be blocked (private windows, strict settings), or hold something odd
  }
}

export function remember(id) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify([id, ...getRecent().filter((r) => r !== id)].slice(0, MAX)));
  } catch {
    // Not remembered; nothing else depends on it.
  }
}
