import { useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { useCommands } from "../commands/registry.jsx";
import Button from "./Button.jsx";
import { EmptyState } from "./Callout.jsx";
import { SearchInput } from "./Field.jsx";
import { Skeleton } from "./Loading.jsx";
import { ArrowDownIcon, ArrowUpIcon, ChevronsUpDownIcon, SearchIcon } from "./icons.jsx";
import "./DataTable.css";

const SKELETON_ROWS = 8;

// DataTable: a list of resources with a filter box and sortable columns.
//
// Each column is an object:
//   key        field name in the row, e.g. "status"
//   label      header text
//   render     optional (row) => what to show; defaults to row[key]
//   sortValue  optional (row) => value to sort by; defaults to row[key]
//   className  optional: "name", "mono", "num" (right-aligned) or "muted-cell"
//
// Other props:
//   rows       the data; null while loading (skeleton rows are shown)
//   noun       what the rows are, plural: "pods"
//   linkTo     optional (row) => URL: the first column becomes a link, and
//              clicking anywhere on the row opens it
//   emptyText  and emptyIcon: shown when there are no rows at all
//   toolbar    extra things on the right of the filter (refresh, updated time)
//   density    "comfortable" (default) or "compact"
//   maxHeight  any CSS length: the table scrolls inside and its header stays put
//   fill       take the rest of the page's height and scroll inside, header
//              staying put (list pages); on phones the page scrolls instead
//   shortcut   a key that jumps to the filter box, e.g. "/"
//
// The filter matches the text of every plain-text column.
export default function DataTable({
  columns,
  rows,
  noun,
  linkTo,
  emptyText,
  emptyIcon,
  toolbar,
  density = "comfortable",
  maxHeight,
  fill = false,
  shortcut,
}) {
  const [filter, setFilter] = useState("");
  const filterRef = useRef(null);

  // The page's main table (the one with a shortcut) offers its filter in
  // the command palette too.
  useCommands(
    "table",
    shortcut
      ? [
          {
            id: "table.filter",
            title: `Filter ${noun}`,
            group: "This page",
            icon: SearchIcon,
            shortcut,
            run: () => filterRef.current?.focus(),
          },
        ]
      : [],
    [noun, shortcut],
  );
  const [sort, setSort] = useState(null); // { key, ascending } or null for the server's order
  const navigate = useNavigate();

  const visible = sortRows(filterRows(rows ?? [], columns, filter), columns, sort);

  function toggleSort(key) {
    // Click once: ascending. Again: descending. Third time: back to default.
    setSort((current) => {
      if (current?.key !== key) return { key, ascending: true };
      return current.ascending ? { key, ascending: false } : null;
    });
  }

  // A click anywhere on a row opens it, except on a real control inside it,
  // or at the end of selecting text. Cmd/Ctrl-click opens a new tab. The
  // name stays a real link, for the keyboard and screen readers.
  function openRow(event, url) {
    if (event.target.closest("a, button, input, select, textarea, label")) return;
    if (window.getSelection()?.toString()) return;
    if (event.metaKey || event.ctrlKey) window.open(url, "_blank");
    else navigate(url);
  }

  return (
    <div className={["data-table", `data-table-${density}`, fill && "data-table-fill"].filter(Boolean).join(" ")}>
      <div className="data-table-toolbar">
        <SearchInput
          className="data-table-filter"
          placeholder={`Filter ${noun}…`}
          label={`Filter ${noun}`}
          value={filter}
          onChange={setFilter}
          shortcut={shortcut}
          inputRef={filterRef}
        />
        <span className="data-table-count">{rows ? countLabel(visible.length, rows.length, noun) : ""}</span>
        {toolbar && <div className="data-table-toolbar-end">{toolbar}</div>}
      </div>

      <div className="data-table-scroll" style={{ maxHeight }}>
        <table>
          <thead>
            <tr>
              {columns.map((col) => (
                // Headers only take the alignment; "mono" and the rest are for the cells.
                <th
                  key={col.key}
                  className={col.className?.includes("num") ? "num" : undefined}
                  aria-sort={ariaSort(sort, col.key)}
                >
                  <button type="button" className="data-table-sort" onClick={() => toggleSort(col.key)}>
                    {col.label}
                    <SortIcon direction={sort?.key === col.key ? (sort.ascending ? "up" : "down") : null} />
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows &&
              visible.map((row) => {
                const url = linkTo?.(row);
                return (
                  <tr
                    key={`${row.namespace ?? ""}/${row.name}`}
                    className={url ? "is-link" : undefined}
                    onClick={url ? (e) => openRow(e, url) : undefined}
                  >
                    {columns.map((col, i) => {
                      const content = col.render ? col.render(row) : row[col.key];
                      return (
                        <td key={col.key} className={col.className}>
                          {url && i === 0 ? (
                            <Link className="data-table-link" to={url}>
                              {content}
                            </Link>
                          ) : (
                            content
                          )}
                        </td>
                      );
                    })}
                  </tr>
                );
              })}

            {!rows &&
              Array.from({ length: SKELETON_ROWS }, (_, r) => (
                <tr key={r} aria-hidden="true">
                  {columns.map((col, c) => (
                    <td key={col.key} className={col.className}>
                      <Skeleton width={skeletonWidth(r, c)} />
                    </td>
                  ))}
                </tr>
              ))}

            {rows && visible.length === 0 && (
              <tr>
                <td colSpan={columns.length} className="data-table-empty">
                  {filter ? (
                    <EmptyState
                      icon={SearchIcon}
                      action={
                        <Button size="sm" onClick={() => setFilter("")}>
                          Clear filter
                        </Button>
                      }
                    >
                      No {noun} match “{filter}”.
                    </EmptyState>
                  ) : (
                    <EmptyState icon={emptyIcon}>{emptyText}</EmptyState>
                  )}
                </td>
              </tr>
            )}
          </tbody>
        </table>
        {!rows && <span className="visually-hidden">Loading {noun}…</span>}
      </div>
    </div>
  );
}

// SimpleTable: a small fixed table in the same style, for detail pages
// (containers, conditions, a Service's ports). No filter or sorting, and
// long text wraps.
//   columns  [{ label, className }]: "num" right-aligns, "mono" for code,
//            "nowrap" keeps a short value on one line
//   rows     [[cell, cell, ...]]; an empty cell shows a dash
//   label    names the table for screen readers, e.g. "Containers"
//
// When a table is wider than the page its box scrolls sideways; the box can
// take keyboard focus (and has a name) so it can be scrolled without a mouse.
export function SimpleTable({ columns, rows, label }) {
  return (
    <div className="data-table data-table-compact simple-table">
      <div className="data-table-scroll" tabIndex={0} role="region" aria-label={label}>
        <table>
          <thead>
            <tr>
              {columns.map((col) => (
                <th key={col.label} className={col.className?.includes("num") ? "num" : undefined}>
                  {col.label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, r) => (
              <tr key={r}>
                {row.map((cell, i) => (
                  <td key={i} className={columns[i]?.className}>
                    {cell === "" || cell == null ? <span className="cell-empty">–</span> : cell}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// An arrow for the sorted column; a faint up-down hint on the others (on hover).
function SortIcon({ direction }) {
  if (direction === "up") return <ArrowUpIcon size={12} className="data-table-sort-icon is-active" />;
  if (direction === "down") return <ArrowDownIcon size={12} className="data-table-sort-icon is-active" />;
  return <ChevronsUpDownIcon size={12} className="data-table-sort-icon" />;
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

// Placeholder widths that vary a little from row to row, so the skeleton
// looks like text rather than a grid of identical bars.
function skeletonWidth(row, col) {
  const base = col === 0 ? 70 : 45;
  return `${base - ((row * 7 + col * 13) % 25)}%`;
}
