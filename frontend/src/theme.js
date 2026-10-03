import { useSyncExternalStore } from "react";

// The colour theme. The choice is "system" (follow the OS), "light" or
// "dark", and is remembered in this browser only (localStorage).
//
// The page's actual theme is the data-theme attribute on <html>, always
// "light" or "dark". A small script in index.html sets it before the page
// first draws, so dark mode doesn't flash white; this file keeps it up to
// date afterwards. The two must agree on STORAGE_KEY and THEME_COLORS.

const STORAGE_KEY = "opscope-theme";
const CHOICES = ["system", "light", "dark"];
// The browser's own UI colour (tab bar on mobile, Safari's toolbar).
const THEME_COLORS = { light: "#f6f3ee", dark: "#101418" };

const systemDark = window.matchMedia("(prefers-color-scheme: dark)");
const listeners = new Set();

export function getThemeChoice() {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    return CHOICES.includes(saved) ? saved : "system";
  } catch {
    return "system"; // storage can be blocked (private windows, strict settings)
  }
}

export function setThemeChoice(choice) {
  try {
    if (choice === "system") localStorage.removeItem(STORAGE_KEY);
    else localStorage.setItem(STORAGE_KEY, choice);
  } catch {
    // Not remembered, but still applied for this visit.
  }
  applyTheme(choice);
}

function applyTheme(choice = getThemeChoice()) {
  const theme = choice === "system" ? (systemDark.matches ? "dark" : "light") : choice;
  document.documentElement.dataset.theme = theme;
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", THEME_COLORS[theme]);
  listeners.forEach((notify) => notify());
}

// Follow the OS when it switches (e.g. automatic dark mode at sunset), and
// other tabs when the choice is changed there.
systemDark.addEventListener("change", () => applyTheme());
window.addEventListener("storage", (event) => {
  if (event.key === STORAGE_KEY) applyTheme();
});

function subscribe(notify) {
  listeners.add(notify);
  return () => listeners.delete(notify);
}

// useThemeChoice returns what the person picked and re-renders when it changes:
//   const choice = useThemeChoice(); // "system" | "light" | "dark"
export function useThemeChoice() {
  return useSyncExternalStore(subscribe, getThemeChoice);
}

// useTheme returns the theme on screen right now, "light" or "dark".
export function useTheme() {
  return useSyncExternalStore(subscribe, () => document.documentElement.dataset.theme);
}
