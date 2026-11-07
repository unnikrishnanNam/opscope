import { useEffect, useRef, useState } from "react";
import { LIVE_MIN_MS, needsTimer, useLive, wants } from "./live.js";

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
// timer, until an error with one of the `stopOn` codes comes back: an answer
// that asking again won't change, like a feature turned off on the server.
//
// With `live` (a resource, like "pods", or a list of them) it reloads when
// the tab's live stream says one of them changed, at most every 2 seconds,
// and the timer only runs while the stream can't say so (see live.js).
// Returns { data, error, loading, updatedAt, reload }.
export function useApi(path, { refreshMs, stopOn, live } = {}) {
  const [state, setState] = useState({ path, data: null, error: null, loading: true, updatedAt: null });
  const [reloads, setReloads] = useState(0);
  const loadRef = useRef(null); // the current path's load, for the timer and the stream
  const lastLoad = useRef(0);
  const stopped = useRef(false);

  const { connection, snapshot } = useLive();
  const timerOn = Boolean(refreshMs) && (!live || needsTimer(snapshot, live));
  const liveKey = [live].flat().filter(Boolean).join(",");

  useEffect(() => {
    if (!path) {
      loadRef.current = null;
      setState({ path, data: null, error: null, loading: false, updatedAt: null });
      return;
    }
    let cancelled = false; // ignore answers that arrive after we moved on
    stopped.current = false;

    // A new path starts empty; a reload of the same path keeps showing the old data.
    setState((s) =>
      s.path === path ? { ...s, loading: true } : { path, data: null, error: null, loading: true, updatedAt: null },
    );

    async function load() {
      lastLoad.current = Date.now();
      try {
        const data = await api(path);
        if (!cancelled) setState({ path, data, error: null, loading: false, updatedAt: new Date() });
      } catch (error) {
        if (!cancelled) setState((s) => ({ ...s, error, loading: false }));
        if (stopOn?.includes(error.code)) stopped.current = true;
      }
    }

    loadRef.current = load;
    load();
    return () => {
      cancelled = true;
    };
  }, [path, reloads]);

  // The timer: always without `live`; with it, only while the stream can't
  // say when this page's kinds change.
  useEffect(() => {
    if (!path || !timerOn) return;
    const timer = setInterval(() => !stopped.current && loadRef.current?.(), refreshMs);
    return () => clearInterval(timer);
  }, [path, refreshMs, timerOn]);

  // The stream: a matching change loads again, no sooner than 2 seconds
  // after the last load; changes in between are covered by one load.
  useEffect(() => {
    if (!path || !liveKey || !connection) return;
    let timer = null;
    const unsubscribe = connection.subscribe((message) => {
      if (timer || stopped.current || !wants(message, liveKey.split(","), path)) return;
      const wait = lastLoad.current + LIVE_MIN_MS - Date.now();
      const run = () => {
        timer = null;
        loadRef.current?.();
      };
      if (wait <= 0) run();
      else timer = setTimeout(run, wait);
    });
    return () => {
      unsubscribe();
      clearTimeout(timer);
    };
  }, [path, liveKey, connection]);

  // Until the effect above catches up, don't hand out data from a previous path.
  const current = state.path === path ? state : { data: null, error: null, loading: Boolean(path), updatedAt: null };
  function reload() {
    setState((s) => ({ ...s, loading: true })); // mark as loading right away, not on the next render
    setReloads((n) => n + 1);
  }

  const { data, error, loading, updatedAt } = current;
  return { data, error, loading, updatedAt, reload };
}
