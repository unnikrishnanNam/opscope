import { setThemeChoice, useThemeChoice } from "../theme.js";
import { SegmentedControl } from "./Toggle.jsx";
import { MonitorIcon, MoonIcon, SunIcon } from "./icons.jsx";

const OPTIONS = [
  { value: "system", label: "System", icon: MonitorIcon },
  { value: "light", label: "Light", icon: SunIcon },
  { value: "dark", label: "Dark", icon: MoonIcon },
];

// ThemeSwitch: follow the OS, or always light or dark. The choice is
// remembered in this browser (see theme.js). Icons only by default, with
// the names on hover and for screen readers.
export default function ThemeSwitch({ showLabels = false, size = "sm" }) {
  return (
    <SegmentedControl
      label="Theme"
      size={size}
      iconOnly={!showLabels}
      value={useThemeChoice()}
      onChange={setThemeChoice}
      options={OPTIONS}
    />
  );
}
