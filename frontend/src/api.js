import { useEffect, useState } from "react";

// ApiError carries the message from the backend:
// {"error": "...", "detail": "...", "code": "..."}. `code` marks special
// cases the UI handles on its own, like "not_installed".
export class ApiError extends Error {
  constructor(message, status, detail, code) {
    super(message);
    this.status = status;
    this.detail = detail;
    this.code = code;
  }
}

// api calls the backend and returns the parsed JSON.
//   api("/clusters")
//   api("/clusters", { method: "POST", body: { name: "lab" } })
export async function api(path, { method = "GET", body } = {}) {
  let res;
  try {
    res = await fetch("/api" + path, {
      method,
      headers: body ? { "Content-Type": "application/json" } : {},
      body: body ? JSON.stringify(body) : undefined,
    });
  } catch {
    throw new ApiError("Can't reach the Opscope server.", 0);
  }

  // 204 No Content has no body.
  const data = res.status === 204 ? null : await res.json().catch(() => null);
  if (!res.ok) {
    throw new ApiError(data?.error ?? `Request failed (HTTP ${res.status})`, res.status, data?.detail, data?.code);
  }
  return data;
}

// useApi loads `path` when a component appears (and when `path` changes).
// Pass `null` as the path to skip loading. With `refreshMs` it reloads on a
// timer. Returns { data, error, loading, updatedAt, reload }.
export function useApi(path, { refreshMs } = {}) {
  const [state, setState] = useState({ path, data: null, error: null, loading: true, updatedAt: null });
  const [reloads, setReloads] = useState(0);

  useEffect(() => {
    if (!path) {
      setState({ path, data: null, error: null, loading: false, updatedAt: null });
      return;
    }
    let cancelled = false; // ignore answers that arrive after we moved on

    // A new path starts empty; a reload of the same path keeps showing the old data.
    setState((s) =>
      s.path === path ? { ...s, loading: true } : { path, data: null, error: null, loading: true, updatedAt: null },
    );

    async function load() {
      try {
        const data = await api(path);
        if (!cancelled) setState({ path, data, error: null, loading: false, updatedAt: new Date() });
      } catch (error) {
        if (!cancelled) setState((s) => ({ ...s, error, loading: false }));
      }
    }

    load();
    const timer = refreshMs ? setInterval(load, refreshMs) : null;
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [path, refreshMs, reloads]);

  // Until the effect above catches up, don't hand out data from a previous path.
  const current = state.path === path ? state : { data: null, error: null, loading: Boolean(path), updatedAt: null };
  function reload() {
    setState((s) => ({ ...s, loading: true })); // mark as loading right away, not on the next render
    setReloads((n) => n + 1);
  }

  const { data, error, loading, updatedAt } = current;
  return { data, error, loading, updatedAt, reload };
}
