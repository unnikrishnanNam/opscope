import { Link } from "react-router";
import { Spinner } from "./Loading.jsx";
import Tooltip from "./Tooltip.jsx";
import "./Button.css";

// Button, or a link that looks like one.
//
//   variant   "secondary" (default), "primary", "quiet" or "danger"
//   size      "md" (default) or "sm"
//   icon      an icon component, e.g. icon={RefreshIcon}
//   label     for icon-only buttons (no children): becomes the button's name
//             for screen readers, and its tooltip
//   loading   shows a spinner over the text (the width stays the same) and
//             blocks clicks until it's done
//   to        makes it a router link; `href` makes it a plain link
//
// Anything else (onClick, type, title, ...) goes to the element itself.
export default function Button({
  variant = "secondary",
  size = "md",
  icon: Icon,
  label,
  loading = false,
  disabled = false,
  to,
  href,
  type = "button",
  className,
  children,
  ...props
}) {
  const iconOnly = !children;
  const classes = [
    "button",
    `button-${variant}`,
    size === "sm" && "button-sm",
    iconOnly && "button-icon-only",
    loading && "button-loading",
    className,
  ]
    .filter(Boolean)
    .join(" ");

  const content = (
    <>
      <span className="button-content">
        {Icon && <Icon size={size === "sm" ? 14 : 16} />}
        {children}
      </span>
      {loading && <Spinner className="button-spinner" size={size === "sm" ? 14 : 16} />}
    </>
  );

  const shared = { className: classes, "aria-label": iconOnly ? label : undefined, ...props };
  let element;
  if (to || href) {
    // Links can't be disabled, so a disabled one becomes plain text.
    if (disabled) {
      element = (
        <span {...shared} aria-disabled="true">
          {content}
        </span>
      );
    } else if (to) {
      element = (
        <Link to={to} {...shared}>
          {content}
        </Link>
      );
    } else {
      element = (
        <a href={href} {...shared}>
          {content}
        </a>
      );
    }
  } else {
    element = (
      <button type={type} disabled={disabled || loading} aria-busy={loading || undefined} {...shared}>
        {content}
      </button>
    );
  }

  return iconOnly && label ? <Tooltip label={label}>{element}</Tooltip> : element;
}
