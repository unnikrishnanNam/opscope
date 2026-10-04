// The registry's state, outside React: which page sources are registered
// right now, and what the sources that load something have loaded. React
// components subscribe to it (registry.jsx), so registering a page's
// commands re-renders only the palette, not the whole app.

// How long loaded data is used before the next opening loads it again.
const MAX_AGE_MS = 30_000;

export function createStore(globalSources) {
  let pageSources = []; // newest first
  const loads = new Map(); // "<source id>\n<key>" -> { data, error, loading, at }
  const listeners = new Set();
  let version = 0;

  function changed() {
    version++;
    listeners.forEach((notify) => notify());
  }

  // The key says which data a source needs for this ctx, e.g. the cluster's
  // id: a new key loads again. null means there's nothing to load (no cluster).
  function keyOf(source, ctx) {
    return source.key ? (source.key(ctx) ?? null) : "";
  }

  // Page sources come first, so the page on screen lists its commands first.
  function sources() {
    return [...pageSources, ...globalSources];
  }

  function entry(source, ctx) {
    const key = keyOf(source, ctx);
    return key === null ? undefined : loads.get(`${source.id}\n${key}`);
  }

  return {
    subscribe(notify) {
      listeners.add(notify);
      return () => listeners.delete(notify);
    },
    version: () => version,

    sources,

    // A page source is added when its component mounts, and its commands
    // are replaced in place when they change, so it keeps its position.
    addPageSource(id) {
      pageSources = [{ id, page: true, commands: [] }, ...pageSources.filter((s) => s.id !== id)];
      changed();
    },
    updatePageSource(id, commands) {
      pageSources = pageSources.map((s) => (s.id === id ? { ...s, commands } : s));
      changed();
    },
    removePageSource(id) {
      pageSources = pageSources.filter((s) => s.id !== id);
      changed();
    },

    // What a source has loaded for this ctx (undefined until it has).
    data: (source, ctx) => entry(source, ctx)?.data,

    // refresh starts loading for every source whose data is missing or
    // older than its maxAge. Old data stays in use while the new one loads,
    // and after a failure.
    refresh(ctx, now = Date.now()) {
      for (const source of sources()) {
        if (!source.load) continue;
        const key = keyOf(source, ctx);
        if (key === null) continue;
        const id = `${source.id}\n${key}`;
        const old = loads.get(id);
        if (old?.loading || (old && now - old.at < (source.maxAge ?? MAX_AGE_MS))) continue;

        loads.set(id, { data: old?.data, error: null, loading: true, at: old?.at ?? 0 });
        changed();
        Promise.resolve()
          .then(() => source.load(ctx))
          .then(
            (data) => loads.set(id, { data: data ?? null, error: null, loading: false, at: Date.now() }),
            (error) => loads.set(id, { data: old?.data, error, loading: false, at: Date.now() }),
          )
          .then(changed);
      }
    },

    // status lists the sources that load, for "Loading objects…" and
    // "Couldn't load objects" lines, and each one's notes about what it
    // loaded ("Secrets aren't searched: ..."):
    // [{ id, label, searchOnly, loading, error, notes }].
    status(ctx) {
      return sources()
        .filter((s) => s.load && keyOf(s, ctx) !== null)
        .map((s) => {
          const e = entry(s, ctx);
          return {
            id: s.id,
            label: s.label ?? s.id,
            searchOnly: Boolean(s.searchOnly),
            loading: e?.loading ?? false,
            error: e?.error ?? null,
            notes: e?.data != null && s.notes ? notesOf(s, ctx, e.data) : [],
          };
        });
    },
  };
}

function notesOf(source, ctx, data) {
  try {
    return source.notes(ctx, data);
  } catch (error) {
    console.error(`Notes from "${source.id}" failed:`, error);
    return [];
  }
}
