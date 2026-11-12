// Namespaces typed by hand, for a user who can't list them: Kubernetes
// doesn't say which namespaces exist to someone who may only read a few.
//
//   typedNamespace(" Team-A ") → "team-a"
//   typedNamespace("a b")      → null (no namespace can be called that)

// A namespace's name is a DNS label: lowercase letters, digits and "-",
// starting and ending with a letter or digit, at most 63 characters.
const LABEL = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

export function typedNamespace(text) {
  const name = text?.trim().toLowerCase() ?? "";
  return LABEL.test(name) ? name : null;
}
