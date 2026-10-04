import { segments } from "../commands/match.js";
import "./Highlight.css";

// Highlight: text with some letters in bold, such as the letters a search
// matched. `positions` are the indexes of those letters, sorted.
//   <Highlight text="api-1" positions={[0, 1, 2]} />  ->  **api**-1
export default function Highlight({ text, positions }) {
  if (!positions?.length) return text;
  return segments(text, positions).map((part, i) =>
    part.match ? (
      <span key={i} className="highlight">
        {part.text}
      </span>
    ) : (
      part.text
    ),
  );
}
