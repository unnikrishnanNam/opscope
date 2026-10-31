// Sorting table rows: what DataTable does when a column header is clicked.

export function sortRows(rows, columns, sort) {
  if (!sort) return rows;
  const column = columns.find((c) => c.key === sort.key);
  const valueOf = column?.sortValue ?? ((row) => row[sort.key]);

  // Copy first: .sort() changes the array in place, and rows belongs to someone else.
  return [...rows].sort((a, b) => {
    const [va, vb] = [valueOf(a), valueOf(b)];
    const result = compare(va, vb);
    // Empty values stay last either way: sorting by usage, fullest first,
    // shouldn't start with every row that has no number.
    if (va == null || vb == null) return result;
    return sort.ascending ? result : -result;
  });
}

// Numbers compare as numbers, text as text ("pod-2" before "pod-10"),
// and empty values go last.
function compare(a, b) {
  if (a == null && b == null) return 0;
  if (a == null) return 1;
  if (b == null) return -1;
  if (typeof a === "number" && typeof b === "number") return a - b;
  return String(a).localeCompare(String(b), undefined, { numeric: true });
}
