import "./Loading.css";

// Spinner: a turning arc in the current text colour, for short waits on a
// button or a small area. Give it a `label` when nothing else on screen says
// what's loading; without one it's hidden from screen readers.
export function Spinner({ size = 16, label, className }) {
  const a11y = label ? { role: "status", "aria-label": label } : { "aria-hidden": true };
  return (
    <svg
      className={["spinner", className].filter(Boolean).join(" ")}
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      {...a11y}
    >
      <circle cx="8" cy="8" r="6.25" stroke="currentColor" strokeWidth="2" opacity="0.2" />
      <path d="M8 1.75A6.25 6.25 0 0 1 14.25 8" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  );
}

// Skeleton: a placeholder block shaped like the content that's on its way,
// so the page doesn't jump when it arrives. Sizes take any CSS length.
export function Skeleton({ width = "100%", height = 12, radius }) {
  return <span className="skeleton" style={{ width, height, borderRadius: radius }} aria-hidden="true" />;
}

// SkeletonText: a few lines of placeholder text, the last one shorter.
export function SkeletonText({ lines = 3 }) {
  return (
    <span className="skeleton-text" aria-hidden="true">
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton key={i} width={i === lines - 1 && lines > 1 ? "60%" : "100%"} />
      ))}
    </span>
  );
}
