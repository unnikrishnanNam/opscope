// kubectl lines for one object, for the command palette's "Copy kubectl …"
// commands. Read-only verbs only (get, describe, logs): Opscope never
// changes a cluster, so it doesn't hand out commands that do either.
//
//   kubectlGet(page, "web", "api-1")       -> "kubectl -n web get pods api-1 -o yaml"
//   kubectlDescribe(page, "", "worker-1")  -> "kubectl describe nodes worker-1"
//   kubectlLogs("web", "api-1", { container: "api", previous: true })
//                                          -> "kubectl -n web logs api-1 -c api --previous"
//
// `page` is the object's page from sections.js: its `kubectl` name if it has
// one (Gateway API kinds, which need their group), otherwise its `resource`.
// The lines use kubectl's current context, which may name the cluster
// differently from Opscope.

export function kubectlGet(page, namespace, name) {
  return line(namespace, "get", typeOf(page), name, "-o", "yaml");
}

export function kubectlDescribe(page, namespace, name) {
  return line(namespace, "describe", typeOf(page), name);
}

export function kubectlLogs(namespace, pod, { container, previous = false } = {}) {
  return line(namespace, "logs", pod, ...(container ? ["-c", container] : []), ...(previous ? ["--previous"] : []));
}

function typeOf(page) {
  return page.kubectl ?? page.resource;
}

function line(namespace, ...args) {
  return ["kubectl", ...(namespace ? ["-n", namespace] : []), ...args].map(quote).join(" ");
}

// Kubernetes names never need quoting, but a word with anything unusual in
// it is quoted for the shell anyway, so a pasted line can't do something else.
function quote(word) {
  return /^[A-Za-z0-9._\/:@=-]+$/.test(word) ? word : `'${word.replaceAll("'", `'\\''`)}'`;
}
