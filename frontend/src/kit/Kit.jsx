import { useState } from "react";
import { setThemeChoice, useTheme, useThemeChoice } from "../theme.js";
import { AppIcon } from "../components/Logo.jsx";
import { Checkbox, SegmentedControl } from "../components/Toggle.jsx";
import { MonitorIcon, MoonIcon, SunIcon } from "../components/icons.jsx";
import { tokenSections } from "./TokensDemo.jsx";
import { basicSections } from "./BasicsDemo.jsx";
import { dataSections } from "./DataDemo.jsx";
import "./kit.css";

// /kit: every design token and component, in every state, in both themes.
// It exists only in development (see App.jsx) and needs no cluster, so
// components can be built and checked before any page uses them.
//
// Each demo file exports a list of sections: { id, title, about, render },
// where render(theme) draws the section for one theme ("light" or "dark").
const sections = [...tokenSections, ...basicSections, ...dataSections];

export default function Kit() {
  const [both, setBoth] = useState(true);

  return (
    <div className="kit">
      <header className="kit-header">
        <AppIcon size={24} title="" />
        <h1 className="kit-title">Opscope kit</h1>
        <span className="kit-hint">Development only</span>
        <div className="kit-controls">
          <Checkbox checked={both} onChange={setBoth}>
            Light and dark side by side
          </Checkbox>
          <ThemeChoice />
        </div>
      </header>

      <nav className="kit-nav" aria-label="Sections">
        {sections.map((s) => (
          <a key={s.id} href={`#${s.id}`}>
            {s.title}
          </a>
        ))}
      </nav>

      {sections.map((s) => (
        <KitSection key={s.id} section={s} both={both} />
      ))}
    </div>
  );
}

// A section shows its content once in the page's theme, or twice (light
// and dark) side by side.
function KitSection({ section, both }) {
  return (
    <section className="kit-section" id={section.id}>
      <h2 className="kit-section-title">{section.title}</h2>
      <p className="kit-section-about">{section.about}</p>
      <div className={both ? "kit-panels" : undefined}>
        {both ? (
          ["light", "dark"].map((theme) => (
            <div key={theme} data-theme={theme} className="kit-panel">
              <div className="kit-panel-label">{theme === "light" ? "Light" : "Dark"}</div>
              {section.render(theme)}
            </div>
          ))
        ) : (
          <div className="kit-panel">
            <PageTheme render={section.render} />
          </div>
        )}
      </div>
    </section>
  );
}

function PageTheme({ render }) {
  return render(useTheme());
}

// Becomes the real ThemeSwitch in phase R3.
function ThemeChoice() {
  return (
    <SegmentedControl
      label="Theme"
      size="sm"
      value={useThemeChoice()}
      onChange={setThemeChoice}
      options={[
        { value: "system", label: "System", icon: MonitorIcon },
        { value: "light", label: "Light", icon: SunIcon },
        { value: "dark", label: "Dark", icon: MoonIcon },
      ]}
    />
  );
}
