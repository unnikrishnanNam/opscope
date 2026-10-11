import { useRef, useState } from "react";
import { Link, useOutletContext, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { useCommands } from "../commands/registry.jsx";
import { problemsByNamespace } from "../diagnosis.js";
import Button from "../components/Button.jsx";
import { Callout } from "../components/Callout.jsx";
import Combobox from "../components/Combobox.jsx";
import { Skeleton } from "../components/Loading.jsx";
import { PageHeader } from "../components/PageHeader.jsx";
import { StateMark } from "../components/TopologyGraph.jsx";
import { TopologyView } from "../components/TopologyMap.jsx";
import { Switch } from "../components/Toggle.jsx";
import { ErrorIcon, NamespaceIcon, TopologyIcon } from "../components/icons.jsx";
import "./Topology.css";

const REFRESH_MS = 10_000;

// The Topology page: the map of the namespace picked at the top, or of one
// app in it (?app=), with everything that's fine left out on request
// (?problems=true). With all namespaces picked, it offers the namespaces,
// those with problems first.
export default function Topology({ page }) {
  const { cluster, reachable } = useOutletContext();
  const [searchParams, setSearchParams] = useSearchParams();
  const namespace = searchParams.get("ns") ?? "";
  const app = searchParams.get("app") ?? "";
  const problemsOnly = searchParams.get("problems") === "true";

  // Workloads whose pods are shown one by one. They belong to a namespace,
  // so picking another one starts grouped again.
  const [expanded, setExpanded] = useState({ namespace, ids: [] });
  const expand = expanded.namespace === namespace ? expanded.ids : [];

  function setParam(key, value) {
    const next = new URLSearchParams(searchParams);
    if (value) next.set(key, value);
    else next.delete(key);
    setSearchParams(next);
  }

  const params = new URLSearchParams({ namespace });
  if (app) params.set("app", app);
  if (problemsOnly) params.set("problems", "true");
  if (expand.length) params.set("expand", expand.join(","));
  const path = reachable && namespace ? `/clusters/${cluster.id}/topology?${params}` : null;
  const { data, error } = useApi(path, { refreshMs: REFRESH_MS });

  // While another view of the same namespace loads (an app picked, problems
  // only), the last one stays on screen rather than a blank.
  const fitKey = `${namespace}|${app}|${problemsOnly}`;
  const last = useRef(null);
  if (data) last.current = { namespace, data, fitKey };
  const shown = data ? last.current : last.current?.namespace === namespace ? last.current : null;

  useCommands(
    "topology",
    namespace
      ? [
          {
            id: "topology.problems",
            title: problemsOnly ? "Show everything on the map" : "Show only problems on the map",
            group: "This page",
            icon: ErrorIcon,
            keywords: ["filter", "failing", "topology"],
            run: () => setParam("problems", problemsOnly ? "" : "true"),
          },
          ...(app
            ? [{ id: "topology.all-apps", title: "Show every app on the map", group: "This page", icon: TopologyIcon, run: () => setParam("app", "") }]
            : []),
        ]
      : [],
    [namespace, problemsOnly, app, searchParams],
  );

  const apps = shown?.data.apps ?? [];
  const where = app ? `app ${app}` : `namespace ${namespace}`;
  // An app from another namespace's link, or one that's gone.
  const unknownApp = Boolean(shown && app && !apps.some((a) => a.name === app));
  const tools = (
    <>
      <Combobox
        label="App"
        showLabel
        size="sm"
        noun="apps"
        value={app}
        onChange={(v) => setParam("app", v)}
        options={[
          { value: "", label: "All apps" },
          ...apps.map((a) => ({ value: a.name, label: a.name, state: a.state })),
          ...(unknownApp ? [{ value: app, label: app }] : []),
        ]}
        renderOption={(o) => (
          <span className="topology-app">
            {o.state ? <StateMark state={o.state} /> : <span className="topology-app-all" aria-hidden="true" />}
            {o.label}
          </span>
        )}
        disabled={apps.length === 0 && !app}
      />
      <Switch checked={problemsOnly} onChange={(on) => setParam("problems", on ? "true" : "")}>
        Problems only
      </Switch>
      {expand.length > 0 && (
        <Button size="sm" variant="quiet" onClick={() => setExpanded({ namespace, ids: [] })}>
          Group all pods
        </Button>
      )}
    </>
  );

  return (
    <section className="topology-page">
      <PageHeader title={page.label} description={page.about} />

      {!namespace && reachable && <NamespaceChooser clusterId={cluster.id} searchParams={searchParams} />}

      {namespace && error && !shown && (
        <Callout tone="error" title="Couldn't draw the map" detail={error.detail}>
          {error.message}
        </Callout>
      )}
      {namespace && !error && !shown && reachable && <Skeleton height={480} />}

      {unknownApp && (
        <Callout tone="warning" title={`There's no app called ${app} in ${namespace}`}>
          <Button size="sm" onClick={() => setParam("app", "")}>
            Show the whole namespace
          </Button>
        </Callout>
      )}

      {shown && !unknownApp && (
        <TopologyView
          clusterId={cluster.id}
          view={shown.data}
          namespace={namespace}
          label={`Map of ${where}`}
          fitKey={shown.fitKey}
          tools={tools}
          empty={problemsOnly ? `Nothing is wrong in ${where}.` : `There's nothing in ${where} to show.`}
          expanded={expand}
          onExpand={(id, on) =>
            setExpanded({ namespace, ids: on ? [...new Set([...expand, id])] : expand.filter((other) => other !== id) })
          }
        />
      )}
    </section>
  );
}

// NamespaceChooser lists the namespaces to map, those with problems first.
// Picking one keeps the rest of the query (problems only).
function NamespaceChooser({ clusterId, searchParams }) {
  const namespaces = useApi(`/clusters/${clusterId}/namespaces`);
  const problems = useApi(`/clusters/${clusterId}/problems`, { refreshMs: REFRESH_MS });
  const counts = problemsByNamespace(problems.data?.problems ?? []);

  if (namespaces.error) {
    return (
      <Callout tone="error" title="Couldn't list the namespaces" detail={namespaces.error.detail}>
        {namespaces.error.message}
      </Callout>
    );
  }
  if (!namespaces.data) return <Skeleton height={160} />;

  const names = namespaces.data
    .map((ns) => ns.name)
    .sort((a, b) => (counts.get(b) ?? 0) - (counts.get(a) ?? 0) || a.localeCompare(b));
  return (
    <div className="topology-choose">
      <p className="topology-choose-text">Choose a namespace to map. Those with problems come first.</p>
      <ul className="topology-namespaces">
        {names.map((name) => {
          const count = counts.get(name) ?? 0;
          return (
            <li key={name}>
              <Link className="topology-namespace" to={{ search: `?${withNamespace(searchParams, name)}` }}>
                <NamespaceIcon size={16} className="topology-namespace-icon" />
                <span className="topology-namespace-name">{name}</span>
                <span className={`topology-namespace-problems ${count ? "has-problems" : ""}`}>
                  {count ? (
                    <>
                      <ErrorIcon size={14} />
                      {count === 1 ? "1 problem" : `${count} problems`}
                    </>
                  ) : problems.data ? (
                    "No problems"
                  ) : (
                    ""
                  )}
                </span>
              </Link>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function withNamespace(searchParams, name) {
  const params = new URLSearchParams(searchParams);
  params.set("ns", name);
  params.delete("app");
  return params.toString();
}
