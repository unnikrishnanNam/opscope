import "./Tag.css";

// Tag: a small label for a kind, a role or a source ("Pod", "init",
// "Environment"). `mono` for technical values like label keys.
export function Tag({ mono = false, title, className, children }) {
  return (
    <span className={["tag", mono && "tag-mono", className].filter(Boolean).join(" ")} title={title}>
      {children}
    </span>
  );
}

// Kbd: a keyboard key, for hints like "Press / to filter".
export function Kbd({ children }) {
  return <kbd className="kbd">{children}</kbd>;
}
