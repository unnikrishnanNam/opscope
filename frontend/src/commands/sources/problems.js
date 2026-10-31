import { ErrorIcon } from "../../components/icons.jsx";

// "Problems" from anywhere: the root causes of what's wrong, at the top of
// the overview's warnings card (the overview goes there for #problems).
export const problems = {
  id: "problems",
  commands: (ctx) =>
    ctx.cluster
      ? [
          {
            id: "go.problems",
            title: "Problems",
            detail: "Overview",
            group: "Go to",
            icon: ErrorIcon,
            keywords: ["why", "failing", "root cause", "diagnosis", "broken"],
            to: `/c/${encodeURIComponent(ctx.cluster.id)}/overview${ctx.nsSearch}#problems`,
          },
        ]
      : [],
};
