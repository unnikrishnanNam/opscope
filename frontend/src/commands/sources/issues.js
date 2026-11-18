import { IssuesIcon } from "../../components/icons.jsx";
import { issuesLink } from "../../pages/issuesRequest.js";

// The Issues page, narrowed: "Issues in shop" when a namespace is picked,
// and the critical ones. ("Issues" itself is a page, from sections.js.)
export const issues = {
  id: "issues",
  commands: (ctx) => {
    if (!ctx.cluster) return [];
    const id = ctx.cluster.id;
    const out = [
      {
        id: "issues.critical",
        title: ctx.namespace ? `Critical issues in ${ctx.namespace}` : "Critical issues",
        group: "Issues",
        icon: IssuesIcon,
        keywords: ["problems", "failing", "broken"],
        to: issuesLink(id, { severity: "critical" }, ctx.nsSearch),
      },
    ];
    if (ctx.namespace) {
      out.unshift({
        id: "issues.namespace",
        title: `Issues in ${ctx.namespace}`,
        group: "Issues",
        icon: IssuesIcon,
        keywords: ["problems", "health"],
        to: issuesLink(id, {}, ctx.nsSearch),
      });
    }
    return out;
  },
};
