import { Link } from "react-router";
import "./Card.css";

// Card: a bordered box with an optional header.
//   title   heading text
//   aside   quiet text after the title, e.g. "newest 8 of 20"
//   action  something on the right of the header, e.g. a button or link
//   flush   no padding around the body, for a table that runs edge to edge
export function Card({ title, aside, action, flush = false, className, children }) {
  return (
    <section className={["card", flush && "card-flush", className].filter(Boolean).join(" ")}>
      {(title || action) && (
        <header className="card-header">
          <h2 className="card-title">
            {title}
            {aside && <span className="card-aside">{aside}</span>}
          </h2>
          {action && <div className="card-action">{action}</div>}
        </header>
      )}
      <div className="card-body">{children}</div>
    </section>
  );
}

// Section: a titled block without a box, for the parts of a detail page.
export function Section({ title, aside, action, children }) {
  return (
    <section className="section">
      <header className="section-header">
        <h2 className="section-title">
          {title}
          {aside && <span className="card-aside">{aside}</span>}
        </h2>
        {action}
      </header>
      {children}
    </section>
  );
}

// StatTile: a label, a big number and a short note underneath (usually how
// many need attention). With `to` the whole tile links to that page.
export function StatTile({ label, value, icon: Icon, to, children }) {
  const body = (
    <>
      <span className="stat-label">
        {Icon && <Icon size={14} />}
        {label}
      </span>
      <span className="stat-value">{value}</span>
      {children && <span className="stat-note">{children}</span>}
    </>
  );
  return to ? (
    <Link className="stat-tile stat-tile-link" to={to}>
      {body}
    </Link>
  ) : (
    <div className="stat-tile">{body}</div>
  );
}
