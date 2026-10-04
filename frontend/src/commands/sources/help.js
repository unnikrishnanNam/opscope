import { KeyboardIcon } from "../../components/icons.jsx";

// The keyboard shortcuts help, from anywhere: press ?, or find it here.
export const help = {
  id: "help",
  commands: [
    {
      id: "help.shortcuts",
      title: "Keyboard shortcuts",
      group: "Help",
      icon: KeyboardIcon,
      keywords: ["keys", "help"],
      shortcut: "?",
      run: (ctx) => ctx.openShortcuts(),
    },
  ],
};
