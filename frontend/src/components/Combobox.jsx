import { useEffect, useId, useRef, useState } from "react";
import { usePopover } from "./Popover.jsx";
import { CheckIcon, ChevronDownIcon, SearchIcon } from "./icons.jsx";
import "./Combobox.css";

// Lists longer than this get a search box.
const SEARCH_FROM = 8;

// Combobox: a button showing the current choice, which opens a list to
// pick from, with a search box for long lists (like the namespace picker).
//
//   label         names it for screen readers, and shows small before the
//                 value when `showLabel` is set ("Namespace  default")
//   value         the chosen option's value
//   options       [{ value, label, ...anything renderOption needs }]
//   onChange      receives the picked value
//   renderOption  optional (option) => content of a row in the list
//   renderValue   optional (option) => content of the button
//   footer        optional, under the list (e.g. a "Manage clusters" link)
//   noun          what the options are, for "No namespaces match …"
//   disabled      with `title` to say why
//
// Keyboard: Enter, Space or Down opens it; typing filters; Up/Down move;
// Enter picks; Escape closes and goes back to the button.
export default function Combobox({
  label,
  showLabel = false,
  value,
  options,
  onChange,
  renderOption,
  renderValue,
  footer,
  noun = "options",
  size = "md",
  align = "start",
  searchable = options.length >= SEARCH_FROM,
  disabled = false,
  title,
}) {
  const id = useId();
  const buttonRef = useRef(null);
  const searchRef = useRef(null);
  const listRef = useRef(null);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const popover = usePopover({
    onClose: ({ returnFocus }) => {
      setQuery("");
      if (returnFocus) buttonRef.current?.focus();
    },
  });

  const needle = query.trim().toLowerCase();
  const visible = needle ? options.filter((o) => o.label.toLowerCase().includes(needle)) : options;
  const selected = options.find((o) => o.value === value);

  // On opening: start on the current choice, and focus the search box (or the list).
  useEffect(() => {
    if (!popover.open) return;
    setActive(
      Math.max(
        0,
        options.findIndex((o) => o.value === value),
      ),
    );
    (searchRef.current ?? listRef.current)?.focus();
  }, [popover.open]);

  // Keep the highlighted row in view while moving with the keyboard.
  useEffect(() => {
    listRef.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView({ block: "nearest" });
  }, [active, popover.open]);

  function pick(option) {
    onChange(option.value);
    popover.close({ returnFocus: true });
  }

  function onListKeyDown(event) {
    const last = visible.length - 1;
    if (event.key === "ArrowDown") setActive((i) => (i >= last ? 0 : i + 1));
    else if (event.key === "ArrowUp") setActive((i) => (i <= 0 ? last : i - 1));
    else if (event.key === "Home" && !searchable) setActive(0);
    else if (event.key === "End" && !searchable) setActive(last);
    else if (event.key === "Enter" && visible[active]) pick(visible[active]);
    else return;
    event.preventDefault();
  }

  const activeId = visible[active] ? `${id}-option-${active}` : undefined;

  return (
    <div ref={popover.ref} className={`popover-anchor combobox combobox-${size}`}>
      <button
        ref={buttonRef}
        type="button"
        className="combobox-button"
        aria-haspopup="listbox"
        aria-expanded={popover.open}
        aria-label={`${label}: ${selected?.label ?? "none chosen"}`}
        disabled={disabled}
        title={title}
        onClick={popover.toggle}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" && !popover.open) {
            e.preventDefault();
            popover.setOpen(true);
          }
        }}
      >
        {showLabel && <span className="combobox-label">{label}</span>}
        <span className="combobox-value">
          {selected ? (renderValue ?? renderOption ?? ((o) => o.label))(selected) : "Choose…"}
        </span>
        <ChevronDownIcon size={size === "sm" ? 14 : 16} className="combobox-chevron" />
      </button>

      {popover.open && (
        <div className={`popover-panel combobox-panel popover-${align}`}>
          {searchable && (
            <div className="combobox-search">
              <SearchIcon size={14} />
              <input
                ref={searchRef}
                type="text"
                role="combobox"
                aria-expanded="true"
                aria-controls={`${id}-list`}
                aria-activedescendant={activeId}
                aria-autocomplete="list"
                aria-label={`Search ${noun}`}
                placeholder={`Search ${noun}…`}
                value={query}
                onChange={(e) => {
                  setQuery(e.target.value);
                  setActive(0);
                }}
                onKeyDown={onListKeyDown}
                autoComplete="off"
                spellCheck={false}
              />
            </div>
          )}
          <ul
            ref={listRef}
            id={`${id}-list`}
            role="listbox"
            aria-label={label}
            className="combobox-list"
            // Without a search box the list itself takes focus and the keys.
            tabIndex={searchable ? undefined : -1}
            aria-activedescendant={searchable ? undefined : activeId}
            onKeyDown={searchable ? undefined : onListKeyDown}
          >
            {visible.map((option, i) => (
              <li
                key={option.value}
                id={`${id}-option-${i}`}
                data-index={i}
                role="option"
                aria-selected={option.value === value}
                className={`combobox-option ${i === active ? "is-active" : ""}`}
                onMouseMove={() => setActive(i)}
                onClick={() => pick(option)}
              >
                <span className="combobox-option-body">{renderOption ? renderOption(option) : option.label}</span>
                {option.value === value && <CheckIcon size={14} className="combobox-check" />}
              </li>
            ))}
            {visible.length === 0 && (
              <li className="combobox-empty" role="presentation">
                No {noun} match “{query}”.
              </li>
            )}
          </ul>
          {footer && (
            // A link in the footer navigates away; close on the way out.
            <div className="combobox-footer" onClick={() => popover.close()}>
              {footer}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
