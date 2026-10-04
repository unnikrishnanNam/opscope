import { useEffect, useId, useRef, useState } from "react";
import { perform, useCommandList } from "../commands/registry.jsx";
import { useModal } from "./Dialog.jsx";
import Highlight from "./Highlight.jsx";
import { Spinner } from "./Loading.jsx";
import { modKey } from "../platform.js";
import { Kbd } from "./Tag.jsx";
import { ChevronRightIcon, SearchIcon, WarningIcon } from "./icons.jsx";
import { useListNavigation } from "./useListNavigation.js";
import "./CommandPalette.css";

// CommandPalette: one box to find and run any command. It shows whatever
// the command registry holds (see commands/registry.jsx) and knows nothing
// about pages, clusters or objects itself.
//
//   open, onClose  controlled, like Dialog: the parent owns `open`
//   ctx            handed to the commands: the cluster, the namespace,
//                  `navigate`, ... Keep its identity between renders (useMemo).
//
// Keyboard: typing searches; Up/Down move; Enter runs the highlighted
// command, or opens its sub-list; Cmd/Ctrl+Enter opens a link in a new tab.
// Escape clears the box, then leaves a sub-list, then closes. Backspace in
// an empty box leaves a sub-list too.
export default function CommandPalette({ open, onClose, ctx }) {
  const modal = useModal(open, onClose);

  // A chosen command runs once the palette has closed. Closing puts focus
  // back where it was before the palette opened, which would undo a
  // command that moves focus itself ("Filter pods"). This effect comes after
  // useModal's, so the <dialog> is already closed when it runs.
  const chosen = useRef(null);
  useEffect(() => {
    if (open || !chosen.current) return;
    const { command, newTab } = chosen.current;
    chosen.current = null;
    perform(command, ctx, { newTab });
  }, [open, ctx]);

  function run(command, newTab) {
    chosen.current = { command, newTab };
    onClose();
  }

  return (
    <dialog className="palette" aria-label="Command palette" {...modal}>
      {/* Only while open, so each opening starts empty and loads fresh data. */}
      {open && <PaletteBody ctx={ctx} onRun={run} />}
    </dialog>
  );
}

function PaletteBody({ ctx, onRun }) {
  const id = useId();
  const inputRef = useRef(null);
  const listRef = useRef(null);
  const [query, setQuery] = useState("");
  const [stack, setStack] = useState([]); // sub-lists opened, the current one last
  const within = stack.at(-1) ?? null;
  const list = useCommandList(ctx, query, within);

  const nav = useListNavigation({
    count: list.items.length,
    listRef,
    homeEnd: false,
    onEnter: (i, event) => choose(list.items[i].command, event),
  });

  // A new query or sub-list starts at the top.
  const { setActive } = nav;
  useEffect(() => setActive(0), [query, within, setActive]);

  function choose(command, { metaKey, ctrlKey } = {}) {
    if (command.items) {
      setStack((s) => [...s, command]);
      setQuery("");
      inputRef.current?.focus();
      return;
    }
    onRun(command, metaKey || ctrlKey);
  }

  function back() {
    setStack((s) => s.slice(0, -1));
    setQuery("");
  }

  function onKeyDown(event) {
    if (event.key === "Escape") {
      if (query) setQuery("");
      else if (stack.length) back();
      else return; // the dialog closes
      event.preventDefault(); // keeps the dialog open
      return;
    }
    if (event.key === "Backspace" && !query && stack.length) {
      event.preventDefault();
      back();
      return;
    }
    nav.onKeyDown(event);
  }

  const optionId = (i) => `${id}-option-${i}`;
  const chip = within && within.title.replace(/…$/, "");
  const loading = list.status.filter((s) => s.loading);
  const failed = list.status.filter((s) => s.error && !s.loading);
  // A search-only source's notes are about its results, so they wait for a query.
  const notes = list.status.filter((s) => query.trim() || !s.searchOnly).flatMap((s) => s.notes);
  let index = 0; // rows are numbered across groups, for the keyboard

  return (
    <div className="palette-content">
      <div className="palette-search">
        <SearchIcon size={16} className="palette-search-icon" />
        {chip && <span className="palette-chip">{chip}</span>}
        <input
          ref={inputRef}
          type="text"
          role="combobox"
          aria-expanded="true"
          aria-controls={`${id}-list`}
          aria-activedescendant={nav.active >= 0 ? optionId(nav.active) : undefined}
          aria-autocomplete="list"
          aria-label={chip ? `Search in ${chip}` : "Search commands"}
          placeholder={chip ? "Search…" : "Search or jump to…"}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKeyDown}
          autoComplete="off"
          spellCheck={false}
          data-autofocus
        />
      </div>

      <div
        ref={listRef}
        id={`${id}-list`}
        role="listbox"
        aria-label={chip ?? "Commands"}
        className="palette-list"
        // Clicking a row mustn't take focus away from the search box.
        onMouseDown={(e) => e.preventDefault()}
      >
        {list.groups.map((group, g) => (
          <div key={group.title} role="group" aria-labelledby={`${id}-group-${g}`} className="palette-group">
            <div id={`${id}-group-${g}`} role="presentation" className="palette-group-title">
              {group.title}
            </div>
            {group.items.map((item) => {
              const i = index++;
              return (
                <Option
                  key={item.command.id}
                  id={optionId(i)}
                  index={i}
                  item={item}
                  active={i === nav.active}
                  onHover={() => setActive(i)}
                  onChoose={(event) => choose(item.command, event)}
                />
              );
            })}
          </div>
        ))}

        {list.items.length === 0 && !loading.length && (
          <p className="palette-empty">{query ? `Nothing matches “${query.trim()}”.` : "Nothing here yet."}</p>
        )}
      </div>

      {(list.total > list.items.length || loading.length > 0 || failed.length > 0 || notes.length > 0) && (
        <div className="palette-status">
          {list.total > list.items.length && (
            <p>
              Showing the best {list.items.length} of {list.total}. Type more to narrow it down.
            </p>
          )}
          {loading.map((s) => (
            <p key={s.id}>
              <Spinner size={12} /> Loading {s.label}…
            </p>
          ))}
          {failed.map((s) => (
            <p key={s.id} title={s.error.detail ?? s.error.message}>
              <WarningIcon size={14} className="palette-status-warn" /> Couldn't load {s.label}: {s.error.message}
            </p>
          ))}
          {notes.map((note) => (
            <p key={note}>{note}</p>
          ))}
        </div>
      )}

      <footer className="palette-footer" aria-hidden="true">
        <span>
          <Kbd>↑</Kbd>
          <Kbd>↓</Kbd> to move
        </span>
        <span>
          <Kbd>↵</Kbd> to open
        </span>
        <span>
          <Kbd>{modKey}</Kbd>
          <Kbd>↵</Kbd> in a new tab
        </span>
        <span className="palette-footer-end">
          {stack.length ? (
            <>
              <Kbd>esc</Kbd> to go back
            </>
          ) : (
            <>
              <Kbd>esc</Kbd> to close
            </>
          )}
        </span>
      </footer>

      {/* Read out after each change, so screen readers know what typing did. */}
      <div className="visually-hidden" aria-live="polite">
        {query && (list.total ? `${list.total} ${list.total === 1 ? "result" : "results"}` : "No results")}
      </div>
    </div>
  );
}

function Option({ id, index, item, active, onHover, onChoose }) {
  const { command, match } = item;
  const Icon = command.icon;
  return (
    <div
      id={id}
      role="option"
      aria-selected={active}
      data-index={index}
      className={`palette-option ${active ? "is-active" : ""}`}
      // Mouse move, not enter: rows scrolling under a still pointer don't steal the highlight.
      onMouseMove={active ? undefined : onHover}
      onClick={onChoose}
    >
      <span className="palette-option-icon">{Icon && <Icon size={16} />}</span>
      <span className="palette-option-title">
        <Highlight text={command.title} positions={match?.title} />
      </span>
      {command.detail && (
        <span className="palette-option-detail">
          <Highlight text={command.detail} positions={match?.detail} />
        </span>
      )}
      {(command.shortcut || command.items) && (
        <span className="palette-option-end">
          {command.shortcut?.split(" ").map((key, i) => (
            <Kbd key={i}>{key}</Kbd>
          ))}
          {command.items && <ChevronRightIcon size={14} />}
        </span>
      )}
    </div>
  );
}
