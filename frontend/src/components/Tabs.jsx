import { useId, useRef } from "react";
import "./Tabs.css";

// Tabs: a row of tabs and the panel for the open one.
//
//   <Tabs label="Pod details" value={tab} onChange={setTab}
//     tabs={[{ value: "summary", label: "Summary" }, { value: "events", label: "Events", count: 3 }]}>
//     {content for the open tab}
//   </Tabs>
//
// Only the open tab is in the Tab order; arrow keys (and Home/End) move
// between tabs and open them, as screen reader users expect.
export default function Tabs({ label, value, onChange, tabs, children }) {
  const id = useId();
  const listRef = useRef(null);

  function onKeyDown(event) {
    const index = tabs.findIndex((t) => t.value === value);
    const next = {
      ArrowRight: (index + 1) % tabs.length,
      ArrowLeft: (index - 1 + tabs.length) % tabs.length,
      Home: 0,
      End: tabs.length - 1,
    }[event.key];
    if (next === undefined) return;
    event.preventDefault();
    onChange(tabs[next].value);
    listRef.current.querySelectorAll('[role="tab"]')[next].focus();
  }

  return (
    <div className="tabs">
      <div ref={listRef} className="tabs-list" role="tablist" aria-label={label} onKeyDown={onKeyDown}>
        {tabs.map((tab) => {
          const selected = tab.value === value;
          return (
            <button
              key={tab.value}
              type="button"
              role="tab"
              id={`${id}-tab-${tab.value}`}
              aria-selected={selected}
              aria-controls={`${id}-panel`}
              tabIndex={selected ? 0 : -1}
              className="tabs-tab"
              onClick={() => onChange(tab.value)}
            >
              {tab.label}
              {tab.count != null && <span className="tabs-count">{tab.count}</span>}
            </button>
          );
        })}
      </div>
      <div id={`${id}-panel`} role="tabpanel" aria-labelledby={`${id}-tab-${value}`} className="tabs-panel">
        {children}
      </div>
    </div>
  );
}
