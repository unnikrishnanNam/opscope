import { cloneElement, useId, useRef } from "react";
import { CloseIcon, ErrorIcon, SearchIcon } from "./icons.jsx";
import "./Field.css";

// Field: a label, one control, and an optional hint and error underneath.
// It connects them for screen readers (the label names the control, the hint
// and error describe it), so the control itself needs no id.
//
//   <Field label="Display name" hint="Shown in the cluster switcher.">
//     <TextInput value={name} onChange={...} />
//   </Field>
//
// `action` goes on the right of the label, e.g. a "Load from file" button.
export function Field({ label, hint, error, action, children }) {
  const id = useId();
  const hintId = hint ? `${id}-hint` : null;
  const errorId = error ? `${id}-error` : null;
  const control = cloneElement(children, {
    id,
    invalid: Boolean(error) || children.props.invalid,
    "aria-describedby": [errorId, hintId].filter(Boolean).join(" ") || undefined,
  });

  return (
    <div className="field">
      <div className="field-header">
        <label htmlFor={id} className="field-label">
          {label}
        </label>
        {action}
      </div>
      {control}
      {error && (
        <div id={errorId} className="field-error">
          <ErrorIcon size={14} />
          {error}
        </div>
      )}
      {hint && (
        <div id={hintId} className="field-hint">
          {hint}
        </div>
      )}
    </div>
  );
}

// The plain controls. Each takes the normal props of its HTML element, plus:
//   size     "md" (default) or "sm"
//   invalid  draws the error border and tells screen readers

export function TextInput({ size = "md", invalid, className, ...props }) {
  return <input className={controlClass("input", size, className)} aria-invalid={invalid || undefined} {...props} />;
}

// `mono` is for kubeconfigs, YAML and other code.
export function Textarea({ mono = false, invalid, className, ...props }) {
  return (
    <textarea
      className={controlClass("input textarea", "md", [mono && "textarea-mono", className])}
      aria-invalid={invalid || undefined}
      {...props}
    />
  );
}

// A styled native <select>, for short fixed lists (like "last 500 lines").
// Long or searchable lists get a Combobox instead (Combobox.jsx).
export function Select({ size = "md", invalid, className, children, ...props }) {
  return (
    <select className={controlClass("select", size, className)} aria-invalid={invalid || undefined} {...props}>
      {children}
    </select>
  );
}

// SearchInput: a text box with a search icon and a clear button. Escape also
// clears it. `onChange` receives the new text, not an event.
//   shortcut  the key that focuses it (e.g. "/"), shown as a hint while the
//             box is empty and announced with aria-keyshortcuts. The key
//             itself is bound by whoever owns the shortcut (DataTable
//             registers it as a command, see commands/keys.js).
//   inputRef  optional ref to the <input>, for focusing it from outside
export function SearchInput({
  value,
  onChange,
  size = "md",
  label = "Filter",
  shortcut,
  inputRef: outerRef,
  className,
  ...props
}) {
  const ownRef = useRef(null);
  const inputRef = outerRef ?? ownRef;

  function clear() {
    onChange("");
    inputRef.current.focus();
  }

  return (
    <span className={["search-input", size === "sm" && "search-input-sm", className].filter(Boolean).join(" ")}>
      <SearchIcon size={size === "sm" ? 14 : 16} className="search-input-icon" />
      <input
        ref={inputRef}
        type="search"
        className={controlClass("input", size)}
        aria-label={label}
        aria-keyshortcuts={shortcut}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape" && value) {
            e.preventDefault();
            clear();
          }
        }}
        {...props}
      />
      {shortcut && !value && (
        <kbd className="kbd search-input-shortcut" aria-hidden="true">
          {shortcut}
        </kbd>
      )}
      {value && (
        // Not in the tab order: Escape does the same from the keyboard.
        <button type="button" className="search-input-clear" onClick={clear} tabIndex={-1} aria-label="Clear">
          <CloseIcon size={14} />
        </button>
      )}
    </span>
  );
}

function controlClass(base, size, extra) {
  return [base, size === "sm" && "control-sm", extra].flat().filter(Boolean).join(" ");
}
