// Small helpers for showing times and numbers the way kubectl does.

// age("2026-10-01T10:00:00Z") -> "1d". Rounds down to one unit.
export function age(timestamp) {
  if (!timestamp) return "–";
  return shortDuration((Date.now() - Date.parse(timestamp)) / 1000);
}

// duration(start, end) -> "3m12s". Without an end, measures up to now.
export function duration(start, end) {
  if (!start) return "–";
  const seconds = ((end ? Date.parse(end) : Date.now()) - Date.parse(start)) / 1000;
  return preciseDuration(seconds);
}

// 45s, 12m, 5h, 123d: one unit, good enough for "how old is this?".
function shortDuration(seconds) {
  const s = Math.max(0, Math.floor(seconds));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400 * 2) return `${Math.floor(s / 3600)}h`;
  return `${Math.floor(s / 86400)}d`;
}

// 45s, 3m12s, 2h5m, 3d4h: two units, for how long something took.
function preciseDuration(seconds) {
  const s = Math.max(0, Math.floor(seconds));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m${s % 60}s`;
  if (s < 86400) return `${Math.floor(s / 3600)}h${Math.floor((s % 3600) / 60)}m`;
  return `${Math.floor(s / 86400)}d${Math.floor((s % 86400) / 3600)}h`;
}

// clock(date) -> "14:03:27", for "Updated at ..." labels.
export function clock(date) {
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
}
