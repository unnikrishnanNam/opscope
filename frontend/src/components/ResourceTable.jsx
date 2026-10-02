import { Fragment, useState } from "react";

// A table for any list of resources, with a text filter and sortable columns.
//
// Each column is an object:
//   key        field name in the row, e.g. "status"
//   label      header text
//   render     optional (row) => what to show; defaults to row[key]
//   sortValue  optional (row) => value to sort by; defaults to row[key]
//   className  optional CSS class for the cells, e.g. "mono" or "num"
//
// The filter matches the text of the name and every plain-text column.
//
// With `expand`, a (row) => JSX function, clicking a row's name opens a
// panel under that row (used to show a secret's keys).
export default function ResourceTable({ columns, rows, noun, emptyText, toolbar, expand }) {
  const [filter, setFilter] = useState("");
  const [sort, setSort] = useState(null); // { key, ascending } or null for the server's order
  const [open, setOpen] = useState({}); // row key -> true when expanded

  function toggleOpen(key) {
    setOpen((prev) => ({ ...prev, [key]: !prev[key] }));
  }

  const visible = sortRows(filterRows(rows ?? [], columns, filter), columns, sort);

  function toggleSort(key) {
    // Click once: ascending. Again: descending. Third time: back to default.
    if (sort?.key !== key) setSort({ key, ascending: true });
    else if (sort.ascending) setSort({ key, ascending: false });
    else setSort(null);
  }

  return (
    <div>
      <div className="table-toolbar">
        <input
          className="input filter"
          type="search"
          placeholder="Filter…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        <span className="muted">
          {rows ? countLabel(visible.length, rows.length, noun) : ""}
        </span>
        <div className="table-toolbar-right">{toolbar}</div>
      </div>

      <div className="table-scroll">
        <table className="table">
          <thead>
            <tr>
              {columns.map((col) => (
                <th key={col.key} className={col.className} aria-sort={ariaSort(sort, col.key)}>
                  <button type="button" className="th-button" onClick={() => toggleSort(col.key)}>
                    {col.label}
                    <span className="sort-arrow">{sort?.key === col.key ? (sort.ascending ? "↑" : "↓") : ""}</span>
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {visible.map((row) => {
              const key = `${row.namespace ?? ""}/${row.name}`;
              const isOpen = expand && open[key];
              return (
                // A Fragment groups the row and its optional panel without adding a DOM element.
                <Fragment key={key}>
                  <tr className={isOpen ? "row-open" : undefined}>
                    {columns.map((col, i) => {
                      const content = col.render ? col.render(row) : row[col.key];
                      return (
                        <td key={col.key} className={col.className}>
                          {expand && i === 0 ? (
                            <button type="button" className="expand-toggle" aria-expanded={!!isOpen} onClick={() => toggleOpen(key)}>
                              <span className="chevron" aria-hidden="true">
                                {isOpen ? "▾" : "▸"}
                              </span>
                              {content}
                            </button>
                          ) : (
                            content
                          )}
                        </td>
                      );
                    })}
                  </tr>
                  {isOpen && (
                    <tr className="row-panel">
                      <td colSpan={columns.length}>{expand(row)}</td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
            {rows && visible.length === 0 && (
              <tr>
                <td colSpan={columns.length} className="table-empty">
                  {filter ? `No ${noun} match “${filter}”.` : emptyText}
                </td>
              </tr>
            )}
            {!rows && (
              <tr>
                <td colSpan={columns.length} className="table-empty">
                  Loading {noun}…
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function filterRows(rows, columns, filter) {
  const needle = filter.trim().toLowerCase();
  if (!needle) return rows;
  return rows.filter((row) =>
    columns.some((col) => {
      const value = row[col.key];
      return typeof value === "string" && value.toLowerCase().includes(needle);
    }),
  );
}

function sortRows(rows, columns, sort) {
  if (!sort) return rows;
  const column = columns.find((c) => c.key === sort.key);
  const valueOf = column?.sortValue ?? ((row) => row[sort.key]);

  // Copy first: .sort() changes the array in place, and rows belongs to someone else.
  return [...rows].sort((a, b) => {
    const result = compare(valueOf(a), valueOf(b));
    return sort.ascending ? result : -result;
  });
}

// Numbers compare as numbers, text as text ("pod-2" before "pod-10"),
// and empty values always go last.
function compare(a, b) {
  if (a == null && b == null) return 0;
  if (a == null) return 1;
  if (b == null) return -1;
  if (typeof a === "number" && typeof b === "number") return a - b;
  return String(a).localeCompare(String(b), undefined, { numeric: true });
}

function countLabel(shown, total, noun) {
  return shown === total ? `${total} ${noun}` : `${shown} of ${total} ${noun}`;
}

function ariaSort(sort, key) {
  if (sort?.key !== key) return undefined;
  return sort.ascending ? "ascending" : "descending";
}
