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

// cores(2000) -> "2 cores", cores(500) -> "0.5 cores". Kubernetes counts CPU
// in millicores: 1000m is one core.
export function cores(millicores) {
  const n = millicores / 1000;
  return `${Number.isInteger(n) ? n : n.toFixed(1)} ${n === 1 ? "core" : "cores"}`;
}

// bytes(3113463808) -> "2.9 GiB". Uses powers of 1024, like Kubernetes' Gi/Mi.
export function bytes(n) {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${i === 0 ? n : n.toFixed(1)} ${units[i]}`;
}

// cpu(63) -> "63m", cpu(1500) -> "1.5 cores". Small values read better in
// millicores, which is also how Kubernetes writes them.
export function cpu(millicores) {
  return millicores < 1000 ? `${millicores}m` : cores(millicores);
}

// percent(1, 3) -> 33. Returns 0 when there's nothing to divide by.
export function percent(part, whole) {
  return whole > 0 ? Math.round((part / whole) * 100) : 0;
}
