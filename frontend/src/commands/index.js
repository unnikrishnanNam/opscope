import { clusters } from "./sources/clusters.jsx";
import { namespaces } from "./sources/namespaces.js";
import { objects } from "./sources/objects.js";
import { pages } from "./sources/pages.js";
import { theme } from "./sources/theme.js";

// Every global command source, in one list. A global source offers commands
// that make sense on any page (pages, clusters, namespaces, the theme,
// objects in the cluster); commands that belong to one page are registered
// by that page with useCommands instead (see registry.jsx).
//
// Adding a global source means writing it in sources/ and adding one line
// here. The order here is the order of their groups in the palette when
// nothing has been typed yet.

export const globalSources = [pages, namespaces, clusters, theme, objects];
