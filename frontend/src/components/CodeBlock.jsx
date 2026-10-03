import { useState } from "react";
import Button from "./Button.jsx";
import { CheckIcon, CopyIcon, WrapTextIcon } from "./icons.jsx";
import "./CodeBlock.css";

// CodeBlock: read-only code (YAML, config values) with line numbers, a copy
// button and a line-wrap switch.
//   code         the text
//   language     "yaml" adds quiet colouring; anything else is plain text
//   title        a short note on the left of the header (without one, the
//                buttons sit beside the code, which suits one-line commands)
//   lineNumbers  on by default
//   maxHeight    scrolls inside after this (any CSS length); none by default
export default function CodeBlock({ code, language, title, lineNumbers = true, maxHeight }) {
  const [wrap, setWrap] = useState(false);
  const [copied, setCopied] = useState(false);
  const lines = code.replace(/\n$/, "").split("\n");

  async function copy() {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // The browser can block copying; the text can still be selected by hand.
    }
  }

  return (
    // Without a title there's no header bar: the buttons sit beside the code.
    <div className={`code-block ${title ? "" : "code-block-bare"}`}>
      <div className="code-block-header">
        {title && <span className="code-block-title">{title}</span>}
        <Button
          variant="quiet"
          size="sm"
          icon={WrapTextIcon}
          label="Wrap lines"
          aria-pressed={wrap}
          onClick={() => setWrap(!wrap)}
        />
        <Button variant="quiet" size="sm" icon={copied ? CheckIcon : CopyIcon} label={copied ? "Copied" : "Copy"} onClick={copy} />
      </div>
      <pre
        className={["code-block-body", wrap && "is-wrapped", lineNumbers && "has-numbers"].filter(Boolean).join(" ")}
        style={{ maxHeight, "--gutter": `${String(lines.length).length + 1}ch` }}
        tabIndex={0} // so it can be scrolled with the keyboard
      >
        <code className="code-block-lines">
          {lines.map((line, i) => (
            // One element for the text, so it's a single cell next to the line number.
            <span key={i} className="code-line">
              <span>{language === "yaml" ? <YamlLine line={line} /> : line}</span>
            </span>
          ))}
        </code>
      </pre>
    </div>
  );
}

function YamlLine({ line }) {
  return yamlParts(line).map((part, i) =>
    part.kind ? (
      <span key={i} className={`yaml-${part.kind}`}>
        {part.text}
      </span>
    ) : (
      part.text
    ),
  );
}

// yamlParts splits one line of YAML into pieces to colour:
//   "  name: web  # the app" -> [plain "  "], [key "name"], [plain ": "],
//                               [plain "web  "], [comment "# the app"]
// It's a line-by-line guess, not a parser: good for Kubernetes YAML, where
// lines inside multi-line strings may occasionally get coloured as keys.
export function yamlParts(line) {
  const parts = [];
  const add = (text, kind = "") => text && parts.push({ text, kind });

  // Indentation and list dashes ("  - ") stay plain.
  const lead = line.match(/^\s*(?:-\s+)*/)[0];
  add(lead);
  let rest = line.slice(lead.length);

  if (rest.startsWith("#")) {
    add(rest, "comment");
    return parts;
  }

  // "key:" followed by a space or the end of the line.
  const key = rest.match(/^("[^"]*"|'[^']*'|[^\s:#'"][^:#]*?):(?=\s|$)/);
  if (key) {
    add(key[1], "key");
    add(":");
    rest = rest.slice(key[0].length);
  }

  const { value, comment } = splitComment(rest);
  const [, before, core, after] = value.match(/^(\s*)(.*?)(\s*)$/);
  add(before);
  add(core, valueKind(core));
  add(after);
  add(comment, "comment");
  return parts;
}

// A "#" starts a comment only outside quotes and after a space, so URLs
// like http://host/#top are left alone.
function splitComment(text) {
  let quote = null;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quote) {
      if (c === quote) quote = null;
    } else if (c === '"' || c === "'") {
      quote = c;
    } else if (c === "#" && (i === 0 || /\s/.test(text[i - 1]))) {
      return { value: text.slice(0, i), comment: text.slice(i) };
    }
  }
  return { value: text, comment: "" };
}

// Quoted text is a string; numbers, true/false and null are literals.
// Plain unquoted words stay the normal text colour, which keeps it calm.
function valueKind(value) {
  if (/^(".*"|'.*')$/.test(value)) return "string";
  if (/^(true|false|null|~|-?\d+(\.\d+)?)$/.test(value)) return "literal";
  return "";
}
