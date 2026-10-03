import { useId } from "react";
import { CheckIcon } from "./icons.jsx";
import "./Toggle.css";

// All three are real <input>s with a custom look drawn next to them, so the
// keyboard and screen readers get the browser's own behaviour for free.

// Checkbox with its label. `onChange` receives true or false.
export function Checkbox({ checked, onChange, children, disabled, title }) {
  return (
    <label className={`checkbox ${disabled ? "is-disabled" : ""}`} title={title}>
      <input
        type="checkbox"
        className="toggle-input"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span className="checkbox-box" aria-hidden="true">
        <CheckIcon size={12} />
      </span>
      {children}
    </label>
  );
}

// Switch: for settings that take effect at once (like following logs).
// Screen readers announce it as on/off. `onChange` receives true or false.
export function Switch({ checked, onChange, children, disabled, title }) {
  return (
    <label className={`switch ${disabled ? "is-disabled" : ""}`} title={title}>
      <input
        type="checkbox"
        role="switch"
        className="toggle-input"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span className="switch-track" aria-hidden="true">
        <span className="switch-thumb" />
      </span>
      {children}
    </label>
  );
}

// SegmentedControl: pick one of a few options, all visible at once.
//
//   <SegmentedControl label="Theme" value={choice} onChange={setChoice}
//     options={[{ value: "light", label: "Light", icon: SunIcon }, ...]} />
//
// With `iconOnly`, labels are hidden on screen but still read out. Arrow keys
// move between options (that's how radio buttons work).
export function SegmentedControl({ label, value, onChange, options, iconOnly = false, size = "md" }) {
  const name = useId();
  return (
    <div className={`segmented ${size === "sm" ? "segmented-sm" : ""}`} role="radiogroup" aria-label={label}>
      {options.map((option) => {
        const Icon = option.icon;
        return (
          <label key={option.value} className="segmented-option" title={iconOnly ? option.label : undefined}>
            <input
              type="radio"
              className="toggle-input"
              name={name}
              value={option.value}
              checked={value === option.value}
              onChange={() => onChange(option.value)}
            />
            <span className="segmented-face">
              {Icon && <Icon size={size === "sm" ? 14 : 16} />}
              <span className={iconOnly ? "visually-hidden" : undefined}>{option.label}</span>
            </span>
          </label>
        );
      })}
    </div>
  );
}
