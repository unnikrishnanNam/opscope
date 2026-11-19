import { useLocation, useOutletContext, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import Button from "../components/Button.jsx";
import { Callout, EmptyState } from "../components/Callout.jsx";
import { Select } from "../components/Field.jsx";
import IssueList from "../components/IssueList.jsx";
import { Skeleton } from "../components/Loading.jsx";
import { PageHeader } from "../components/PageHeader.jsx";
import Tabs from "../components/Tabs.jsx";
import { SegmentedControl } from "../components/Toggle.jsx";
import { IssuesIcon, SuccessIcon } from "../components/icons.jsx";
import { clock, singular } from "../format.js";
import { allPages, nsQuery } from "../sections.js";
import { choices, filtered, issuesPath, issuesRequest, withFilter } from "./issuesRequest.js";
import "./Issues.css";

// Without live updates (see live.js), or for an answer worked out on request.
const REFRESH_MS = 10_000;

const SEVERITIES = [
  { value: "", label: "All" },
  { value: "critical", label: "Critical" },
  { value: "warning", label: "Warning" },
  { value: "info", label: "Info" },
];

// The Issues page: the Signal engine's open signals, worst first, a
// workload's pods grouped; or the last hour's resolved ones. Filters live
// in the URL (issuesRequest.js), so links from the overview land filtered.
export default function Issues({ page }) {
  const { cluster, reachable } = useOutletContext();
  const [params, setParams] = useSearchParams();
  const search = nsQuery(useLocation().search);
  const r = issuesRequest(params);
  const path = reachable ? issuesPath(cluster.id, r) : null;
  const { data, error } = useApi(path, { refreshMs: REFRESH_MS, live: "issues" });

  const set = (name, value) => setParams(withFilter(params, name, value), { replace: name !== "tab" });
  const all = data?.groups ?? [];
  const shown = filtered(all, r);
  // Each filter offers what the others let through, with how many.
  const options = {
    types: choices(filtered(all, { ...r, types: [] })).types,
    kinds: choices(filtered(all, { ...r, kind: "" })).kinds,
  };
  const filtering = r.severity || r.types.length || r.kind;

  return (
    <section className="issues-page">
      <PageHeader title={page.label} description={page.about} />

      {data && <About data={data} />}

      <Tabs
        label="Issues"
        value={r.tab}
        onChange={(tab) => set("tab", tab === "open" ? "" : tab)}
        tabs={[
          { value: "open", label: "Open", count: data?.open },
          { value: "resolved", label: "Resolved in the last hour", count: data?.resolved },
        ]}
      >
        <div className="issues-filters">
          <SegmentedControl
            label="Severity"
            size="sm"
            value={r.severity}
            onChange={(v) => set("severity", v)}
            options={SEVERITIES}
          />
          <FilterSelect
            label="Type"
            value={r.types.join(",")}
            all="All types"
            options={withCurrent(options.types, r.types.join(","), labelOfTypes(r.types, all))}
            onChange={(v) => set("type", v)}
          />
          <FilterSelect
            label="Kind"
            value={r.kind}
            all="All kinds"
            options={withCurrent(options.kinds, r.kind, kindLabel(r.kind))}
            onChange={(v) => set("kind", v)}
          />
          {filtering && (
            <Button
              size="sm"
              variant="quiet"
              onClick={() => {
                let next = params;
                for (const f of ["severity", "type", "kind"]) next = withFilter(next, f, "");
                setParams(next, { replace: true });
              }}
            >
              Clear filters
            </Button>
          )}
          {data && (
            <span className="issues-shown" aria-live="polite">
              {filtering ? `${shown.length} of ${all.length}` : all.length} {all.length === 1 ? "issue" : "issues"}
            </span>
          )}
        </div>

        {error && !data ? (
          <Callout tone="error" title="Couldn't load the issues" detail={error.detail}>
            {error.message}
          </Callout>
        ) : !data ? (
          <Skeleton height={240} />
        ) : shown.length === 0 ? (
          <Nothing tab={r.tab} namespace={r.namespace} filtering={filtering} partial={data.partial ?? []} />
        ) : (
          <IssueList
            groups={shown}
            clusterId={cluster.id}
            search={search}
            showNamespace={!r.namespace}
            open={r.signal || undefined}
          />
        )}
      </Tabs>
    </section>
  );
}

// About says how the answer was made, and what it couldn't see: kept live
// since some time, or worked out for this page; kinds it couldn't read.
function About({ data }) {
  const partial = data.partial ?? [];
  const forbidden = partial.filter((p) => p.code === "forbidden");
  return (
    <div className="issues-about">
      {data.built === "on_request" ? (
        <Callout compact>
          Worked out from the cluster when you open this page, and again every 10 seconds: Opscope isn't keeping a live
          copy of this cluster, so there's no history and nothing under Resolved.
        </Callout>
      ) : (
        data.historySince && (
          <p className="issues-since">
            Watching since {clock(new Date(data.historySince))}. Issues are kept in memory, so they start again when
            Opscope restarts.
          </p>
        )
      )}
      {forbidden.length > 0 && !cantReadPods(partial) && (
        <Callout compact tone="warning">
          Opscope can't read {someKinds(forbidden)}, so it can't check them:{" "}
          {forbidden.length === 1 && forbidden[0].resource === "secrets"
            ? "certificates nearing expiry, and Secrets that pods name, aren't checked."
            : "problems with them won't show here."}
        </Callout>
      )}
    </div>
  );
}

function Nothing({ tab, namespace, filtering, partial }) {
  const where = namespace ? ` in ${namespace}` : "";
  // Without pods there's nothing to say "healthy" about: most problems show
  // on pods. A user allowed into only some namespaces is here when looking
  // across all of them.
  if (cantReadPods(partial)) {
    return (
      <EmptyState icon={IssuesIcon} title={`Opscope isn't allowed to read pods${where}`}>
        So it can't tell what's wrong {namespace ? "here" : "across the cluster"}.{" "}
        {namespace
          ? "Ask for read access to this namespace, or pick another in the namespace picker."
          : "It may only be allowed into some namespaces: pick one in the namespace picker, or type its name there."}
      </EmptyState>
    );
  }
  if (filtering) {
    return (
      <EmptyState title={`No ${tab} issues match these filters${where}`}>
        Clear the filters to see every {tab === "open" ? "open" : "resolved"} issue.
      </EmptyState>
    );
  }
  if (tab === "resolved") {
    return <EmptyState title={`Nothing resolved in the last hour${where}`}>Issues that clear show here for an hour.</EmptyState>;
  }
  return (
    <EmptyState icon={SuccessIcon} title={`No open issues${where}`}>
      Every object Opscope checks looks healthy.
    </EmptyState>
  );
}

function cantReadPods(partial) {
  return partial.some((p) => p.resource === "pods" && p.code === "forbidden");
}

// someKinds names up to four kinds and counts the rest: "Pods, Services,
// Jobs, Nodes and 12 more".
function someKinds(kinds) {
  const names = kinds.map((p) => kindLabel(p.resource, true));
  if (names.length <= 4) return names.join(", ");
  return `${names.slice(0, 4).join(", ")} and ${names.length - 4} more`;
}

// FilterSelect: a labelled native select with an "All" choice.
function FilterSelect({ label, value, all, options, onChange }) {
  return (
    <label className="issues-filter">
      <span className="issues-filter-label">{label}</span>
      <Select size="sm" value={value} onChange={(e) => onChange(e.target.value)}>
        <option value="">{all}</option>
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
            {o.count != null ? ` (${o.count})` : ""}
          </option>
        ))}
      </Select>
    </label>
  );
}

// withCurrent keeps a filter's value among the choices when nothing has
// it now, so the select still shows what's picked.
function withCurrent(options, value, label) {
  if (!value || options.some((o) => o.value === value)) return options;
  return [...options, { value, label }];
}

function labelOfTypes(types, groups) {
  return types.map((t) => groups.find((g) => g.type === t)?.title ?? t.replaceAll("_", " ")).join(", ");
}

// kindLabel: "deployments" → "Deployments" (or "Deployment" when one).
function kindLabel(resource, plural = false) {
  if (!resource) return "";
  const page = allPages.find((p) => p.resource === resource);
  const label = page?.label ?? resource;
  return plural ? label : singular(label);
}
