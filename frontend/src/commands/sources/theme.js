import { MonitorIcon, MoonIcon, SunIcon } from "../../components/icons.jsx";
import { getThemeChoice, setThemeChoice } from "../../theme.js";

// The theme switch from the sidebar's footer: system, light or dark.
const CHOICES = [
  { choice: "light", title: "Light theme", icon: SunIcon },
  { choice: "dark", title: "Dark theme", icon: MoonIcon },
  { choice: "system", title: "Follow the system theme", icon: MonitorIcon },
];

export const theme = {
  id: "theme",
  commands: () => {
    const current = getThemeChoice();
    return CHOICES.map(({ choice, title, icon }) => ({
      id: `theme.${choice}`,
      title,
      detail: choice === current ? "current" : undefined,
      group: "Theme",
      icon,
      keywords: ["theme", "mode", "appearance"],
      run: () => setThemeChoice(choice),
    }));
  },
};
