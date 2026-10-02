import { NavLink } from "react-router";
import { sections } from "../sections.js";

export default function Sidebar() {
  return (
    <aside className="sidebar">
      <div className="brand">
        <img src="/favicon.svg" alt="" width="22" height="22" />
        <span>OpScope</span>
      </div>

      <nav>
        {sections.map((section) => (
          <div key={section.group} className="nav-group">
            <div className="nav-group-title">{section.group}</div>
            {section.items.map((item) => (
              // NavLink adds the "active" class when its path matches the URL.
              <NavLink key={item.path} to={item.path} className="nav-link">
                {item.label}
              </NavLink>
            ))}
          </div>
        ))}
      </nav>
    </aside>
  );
}
