import { Link } from "react-router";
import { ChevronRightIcon } from "./icons.jsx";
import "./PageHeader.css";

// Breadcrumbs: where this page sits, e.g. Workloads › Pods › api-7d9f.
//   items  [{ label, to }]: earlier items link back (when they have `to`),
//          the last one is the current page
export function Breadcrumbs({ items }) {
  return (
    <nav className="breadcrumbs" aria-label="Breadcrumb">
      <ol>
        {items.map((item, i) => {
          const current = i === items.length - 1;
          return (
            <li key={i}>
              {i > 0 && <ChevronRightIcon size={14} className="breadcrumbs-sep" />}
              {current ? (
                <span className="breadcrumbs-current" aria-current="page" title={item.label}>
                  {item.label}
                </span>
              ) : item.to ? (
                <Link to={item.to}>{item.label}</Link>
              ) : (
                <span>{item.label}</span>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

// PageHeader: the top of a page.
//   title        the page or object name
//   description  one sentence underneath (or facts like "Namespace web · 3d old")
//   before       shown before the title, e.g. a kind tag ("Pod")
//   after        shown after the title, e.g. a status badge
//   actions      buttons on the right
//   meta         quiet text on the right, e.g. "Updated 10:42:07"
export function PageHeader({ title, description, before, after, actions, meta }) {
  return (
    <header className="page-header-block">
      <div className="page-header-main">
        <div className="page-header-title-row">
          {before}
          <h1 className="page-header-title">{title}</h1>
          {after}
        </div>
        {description && <div className="page-header-description">{description}</div>}
      </div>
      {(actions || meta) && (
        <div className="page-header-side">
          {meta && <span className="page-header-meta">{meta}</span>}
          {actions}
        </div>
      )}
    </header>
  );
}
