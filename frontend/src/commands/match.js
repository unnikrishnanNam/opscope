// Fuzzy matching for the command palette: does a query match a command,
// how well, and which letters matched (so they can be shown in bold).
//
// The query is split into words, and every word has to match somewhere in
// the command's title, keywords or detail. One word matches one piece of
// text in the first of these ways that works:
//
//   exact       the whole text           "pods" in "Pods"
//   prefix      the start of the text    "dep"  in "Deployments"
//   word start  the start of a word      "api"  in "checkout-api-6b7c"
//   inside      anywhere                 "ploy" in "Deployments"
//   letters     its letters in order,    "dply" in "Deployments"
//               from the start of a word
//
// Scores fall in bands, so a better kind of match always wins: exact 100,
// prefix 80–90, word start 60–70, inside 40–50, letters 1–35. Within a
// band, a word that covers more of the text scores a little higher, so
// "pod" ranks "Pods" above "PodDisruptionBudgets".

const EXACT = 100;
const PREFIX = 80;
const WORD_START = 60;
const INSIDE = 40;
const LETTERS = 20;
const LETTERS_MAX = 35;

// Where a word was found counts for less outside the title.
const WEIGHTS = { title: 1, keywords: 0.9, scope: 0.9, detail: 0.7 };

const alnum = /[a-z0-9]/i;
const lower = /[a-z]/;
const upper = /[A-Z]/;
const letter = /[a-z]/i;
const digit = /[0-9]/;

// queryWords("  Po API ") -> ["po", "api"]
export function queryWords(query) {
  return query.trim().toLowerCase().split(/\s+/).filter(Boolean);
}

// A word starts at the beginning of the text, after punctuation or a space
// ("web/api-1"), at a capital after a small letter ("StatefulSets"), and
// at a digit after a letter ("worker2").
function isWordStart(text, i) {
  if (i === 0) return true;
  const prev = text[i - 1];
  const cur = text[i];
  if (!alnum.test(prev)) return alnum.test(cur);
  return (lower.test(prev) && upper.test(cur)) || (letter.test(prev) && digit.test(cur));
}

function range(from, to) {
  return Array.from({ length: to - from }, (_, i) => from + i);
}

// matchWord("dply", "Deployments") -> { score: 21, positions: [0, 2, 3, 5] }
// The word must already be lower case (queryWords does that). Returns null
// when the word isn't in the text at all.
export function matchWord(word, text) {
  const haystack = text.toLowerCase();
  const n = word.length;
  const fit = 10 * (n / text.length); // 0–10: how much of the text the word covers

  if (haystack === word) return { score: EXACT, positions: range(0, n) };
  if (haystack.startsWith(word)) return { score: PREFIX + fit, positions: range(0, n) };

  let inside = -1;
  for (let i = haystack.indexOf(word); i !== -1; i = haystack.indexOf(word, i + 1)) {
    if (isWordStart(text, i)) return { score: WORD_START + fit, positions: range(i, i + n) };
    if (inside === -1) inside = i;
  }
  if (inside !== -1) return { score: INSIDE + fit, positions: range(inside, inside + n) };

  return matchLetters(word, text, haystack);
}

// The word's letters in order, starting at the start of a word. Each start
// is tried and the best one kept: letters next to each other or at word
// starts score higher, long gaps lower.
function matchLetters(word, text, haystack) {
  let best = null;
  for (let start = haystack.indexOf(word[0]); start !== -1; start = haystack.indexOf(word[0], start + 1)) {
    if (!isWordStart(text, start)) continue;

    const positions = [start];
    for (let k = 1, at = start; k < word.length; k++) {
      at = haystack.indexOf(word[k], at + 1);
      if (at === -1) return best; // a later start has even less text left
      positions.push(at);
    }

    let score = LETTERS;
    for (let k = 1; k < positions.length; k++) {
      if (positions[k] === positions[k - 1] + 1) score += 2;
      else if (isWordStart(text, positions[k])) score += 3;
    }
    score -= 0.5 * (positions.at(-1) - start + 1 - positions.length); // letters skipped
    score = Math.max(1, Math.min(LETTERS_MAX, score));

    if (!best || score > best.score) best = { score, positions };
  }
  return best;
}

// matchCommand(["po", "api"], command) -> { score, title: [...], detail: [...] }
// Every word must match the title, a keyword, a scope word or the detail;
// each counts where it matched best. `title` and `detail` are the matched
// letter positions in those texts, for bold. Returns null if a word doesn't
// match.
//
// Scope words narrow a search but don't find anything by themselves: a pod
// has the scope ["pods", "pod", "po"], so "po api" finds pods called api,
// while "pod" alone finds the Pods page, not every pod.
export function matchCommand(words, command) {
  const fields = [["title", command.title]];
  for (const keyword of command.keywords ?? []) fields.push(["keywords", keyword]);
  for (const word of command.scope ?? []) fields.push(["scope", word]);
  if (command.detail) fields.push(["detail", command.detail]);

  let score = 0;
  let onlyScope = true;
  const found = { title: new Set(), detail: new Set() };
  for (const word of words) {
    let best = null;
    for (const [field, text] of fields) {
      const match = matchWord(word, text);
      if (!match) continue;
      const weighted = match.score * WEIGHTS[field];
      if (!best || weighted > best.score) best = { score: weighted, field, positions: match.positions };
    }
    if (!best) return null;
    score += best.score;
    if (best.field !== "scope") onlyScope = false;
    // Keywords and scope words aren't shown, so there's nothing to make bold.
    if (found[best.field]) best.positions.forEach((p) => found[best.field].add(p));
  }
  if (onlyScope) return null;

  const sorted = (set) => [...set].sort((a, b) => a - b);
  return { score, title: sorted(found.title), detail: sorted(found.detail) };
}

// segments("api-1", [0, 1, 4]) ->
//   [{ text: "ap", match: true }, { text: "i-", match: false }, { text: "1", match: true }]
// Positions must be sorted (matchCommand's are).
export function segments(text, positions) {
  const marked = new Set(positions);
  const parts = [];
  for (let i = 0; i < text.length; i++) {
    const match = marked.has(i);
    const last = parts.at(-1);
    if (last && last.match === match) last.text += text[i];
    else parts.push({ text: text[i], match });
  }
  return parts;
}
