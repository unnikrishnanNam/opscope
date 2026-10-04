import { useEffect, useState } from "react";

// useListNavigation: which row of a list is highlighted, moved with the
// keyboard while focus stays somewhere else (usually a search box, whose
// aria-activedescendant points at the row). Combobox and the command
// palette both use it.
//
//   const listRef = useRef(null);
//   const nav = useListNavigation({ count: rows.length, listRef, onEnter: (i, event) => pick(rows[i]) });
//   <input onKeyDown={nav.onKeyDown} aria-activedescendant={`row-${nav.active}`} />
//   <ul ref={listRef}>{rows.map((row, i) => <li data-index={i} ...>)}</ul>
//
// Up and Down move and wrap round. Home and End jump to the ends, unless
// `homeEnd` is false (in a search box they move the text cursor instead).
// Enter calls onEnter with the row's index and the key event. The
// highlighted row is scrolled into view; rows need data-index. `open` is
// for lists that appear and disappear: opening one scrolls to its row too.
//
// `active` is always a row that exists (or -1 for an empty list), even
// right after the list got shorter.
export function useListNavigation({ count, listRef, onEnter, homeEnd = true, open = true }) {
  const [index, setActive] = useState(0);
  const active = Math.min(index, count - 1);

  useEffect(() => {
    listRef.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView({ block: "nearest" });
  }, [active, open, listRef]);

  function onKeyDown(event) {
    if (!count) return;
    const last = count - 1;
    if (event.key === "ArrowDown") setActive(active >= last ? 0 : active + 1);
    else if (event.key === "ArrowUp") setActive(active <= 0 ? last : active - 1);
    else if (event.key === "Home" && homeEnd) setActive(0);
    else if (event.key === "End" && homeEnd) setActive(last);
    else if (event.key === "Enter") onEnter(active, event);
    else return;
    event.preventDefault();
  }

  return { active, setActive, onKeyDown };
}
