import { keysOf } from "../commands/keys.js";
import { modKey } from "../platform.js";
import { Dialog } from "./Dialog.jsx";
import { Kbd } from "./Tag.jsx";
import "./ShortcutsHelp.css";

// ShortcutsHelp: every keyboard shortcut that works right now, built from
// the same commands the keys are bound to (useShortcutCommands), so the two
// can't disagree. Opened with ? or from the palette.
//   commands  the commands with a shortcut, in registry order
export default function ShortcutsHelp({ open, onClose, commands }) {
  const groups = new Map([["Anywhere", [{ id: "palette", title: "Search and commands", keys: [`${modKey}K`] }]]]);
  for (const command of commands) {
    const group = command.group ?? "Other";
    if (!groups.has(group)) groups.set(group, []);
    groups.get(group).push({ id: command.id, title: command.title, keys: keysOf(command.shortcut) });
  }

  return (
    <Dialog open={open} onClose={onClose} title="Keyboard shortcuts">
      <p className="shortcuts-about">
        They work anywhere except while typing in a field. Keys shown with “then” are pressed one after the other.
      </p>
      {[...groups].map(([title, rows]) => (
        <section key={title} className="shortcuts-group">
          <h3 className="shortcuts-group-title">{title}</h3>
          <dl className="shortcuts-list">
            {rows.map((row) => (
              <div key={row.id} className="shortcuts-row">
                <dt>{row.title}</dt>
                <dd>
                  {row.keys.map((key, i) => (
                    <span key={i} className="shortcuts-key">
                      {i > 0 && <span className="shortcuts-then">then</span>}
                      {key === `${modKey}K` ? (
                        <>
                          <Kbd>{modKey}</Kbd>
                          <Kbd>K</Kbd>
                        </>
                      ) : (
                        <Kbd>{key}</Kbd>
                      )}
                    </span>
                  ))}
                </dd>
              </div>
            ))}
          </dl>
        </section>
      ))}
    </Dialog>
  );
}
