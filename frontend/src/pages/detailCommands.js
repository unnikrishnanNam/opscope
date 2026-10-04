import { copyText } from "../clipboard.js";
import { CopyIcon, EventsIcon, FileTextIcon, InfoIcon, LogsIcon, NodesIcon } from "../components/icons.jsx";
import { kubectlDescribe, kubectlGet, kubectlLogs } from "../kubectl.js";
import { allPages, detailPath } from "../sections.js";

const GROUP = "This page";
const TAB_ICONS = { summary: InfoIcon, yaml: FileTextIcon, events: EventsIcon, logs: LogsIcon };

// detailCommands lists what the command palette offers on one object's
// page (registered by ResourceDetail with useCommands): its other tabs,
// copying its name, YAML or a kubectl line, and the pages it links to (its
// namespace's list, its owners, a pod's node).
//
//   d        the object, from the detail endpoint
//   page     its page in sections.js
//   tab      the open tab; `tabs` all of them, and openTab(value) opens one
//   search   "?ns=..." to keep on links
export function detailCommands({ d, page, clusterId, tab, tabs, openTab, search }) {
  const { namespace, name } = d;
  const commands = [];

  for (const t of tabs) {
    if (t.value === tab) continue;
    commands.push({
      id: `detail.tab.${t.value}`,
      title: `Show ${t.value === "yaml" ? "YAML" : t.label.toLowerCase()}`,
      group: GROUP,
      icon: TAB_ICONS[t.value],
      keywords: ["tab"],
      run: () => openTab(t.value),
    });
  }

  commands.push(
    { id: "detail.copy.name", title: "Copy name", detail: name, group: GROUP, icon: CopyIcon, run: () => copyText(name, "the name") },
    { id: "detail.copy.yaml", title: "Copy YAML", group: GROUP, icon: CopyIcon, run: () => copyText(d.yaml, "the YAML") },
    kubectl("get", kubectlGet(page, namespace, name)),
    kubectl("describe", kubectlDescribe(page, namespace, name)),
  );
  // On the Logs tab, the log viewer offers its own, for the container shown.
  if (page.resource === "pods" && tab !== "logs") commands.push(kubectl("logs", kubectlLogs(namespace, name)));

  if (namespace) {
    commands.push({
      id: "detail.namespace-list",
      title: `${page.label} in ${namespace}`,
      group: GROUP,
      icon: page.icon,
      to: `/c/${clusterId}/${page.path}?ns=${encodeURIComponent(namespace)}`,
    });
  }

  for (const owner of d.owners) {
    const link = owner.resource && detailPath(clusterId, owner.resource, namespace, owner.name);
    if (!link) continue;
    commands.push({
      id: `detail.owner.${owner.resource}.${owner.name}`,
      title: `Go to ${owner.kind} ${owner.name}`,
      group: GROUP,
      icon: allPages.find((p) => p.resource === owner.resource)?.icon,
      keywords: ["owner"],
      to: link + search,
    });
  }

  const node = page.resource === "pods" && d.fields.find((f) => f.label === "Node")?.value;
  if (node) {
    commands.push({
      id: "detail.node",
      title: `Go to node ${node}`,
      group: GROUP,
      icon: NodesIcon,
      to: detailPath(clusterId, "nodes", "", node) + search,
    });
  }

  return commands;
}

function kubectl(verb, line) {
  return {
    id: `detail.kubectl.${verb}`,
    title: `Copy kubectl ${verb}`,
    detail: line,
    group: GROUP,
    icon: CopyIcon,
    keywords: ["command"],
    run: () => copyText(line, "the kubectl command"),
  };
}
