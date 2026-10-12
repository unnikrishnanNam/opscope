import { useRef, useState } from "react";
import { useOutletContext, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { useCommands } from "../commands/registry.jsx";
import Button from "../components/Button.jsx";
import { Callout } from "../components/Callout.jsx";
import Combobox from "../components/Combobox.jsx";
import { Skeleton } from "../components/Loading.jsx";
import { PageHeader } from "../components/PageHeader.jsx";
import { StateMark } from "../components/TopologyGraph.jsx";
import { TopologyView } from "../components/TopologyMap.jsx";
import { Switch } from "../components/Toggle.jsx";
import { ErrorIcon, TopologyIcon } from "../components/icons.jsx";
import "./Topology.css";

const REFRESH_MS = 10_000;
// The cluster view: apps and namespaces joined to nothing in a grid, the
// machines in a strip, both under the columns.
const CLUSTER = { grid: ["apps", "namespaces"], strip: ["nodes"] };

// The Topology page: the map of the namespace picked at the top, or of one
// app in it (?app=), with everything that's fine left out on request
// (?problems=true). With all namespaces picked, the whole cluster: its
// namespaces holding their apps, the entry points they share, and the nodes.
export default function Topology({ page }) {
  const { cluster, reachable } = useOutletContext();
  const [searchParams, setSearchParams] = useSearchParams();
  const namespace = searchParams.get("ns") ?? "";
  const app = searchParams.get("app") ?? "";
  const problemsOnly = searchParams.get("problems") === "true";

  // Workloads whose pods are shown one by one; for the whole cluster,
  // namespaces shown open although nothing is wrong in them. They belong
  // to what's picked, so picking something else starts afresh.
  const [expanded, setExpanded] = useState({ namespace, ids: [] });
  const wholeCluster = !namespace;
  const expand = expanded.namespace === namespace ? expanded.ids : [];

  function setParam(key, value) {
    const next = new URLSearchParams(searchParams);
    if (value) next.set(key, value);
    else next.delete(key);
    setSearchParams(next);
  }

  const params = new URLSearchParams();
  if (namespace) params.set("namespace", namespace);
  if (namespace && app) params.set("app", app);
  if (problemsOnly) params.set("problems", "true");
  if (expand.length) params.set("expand", expand.join(","));
  const path = reachable ? `/clusters/${cluster.id}/topology?${params}` : null;
  const { data, error } = useApi(path, { refreshMs: REFRESH_MS });

  // While another view of the same namespace loads (an app picked, problems
  // only), the last one stays on screen rather than a blank.
  const fitKey = `${namespace}|${app}|${problemsOnly}`;
  const last = useRef(null);
  if (data) last.current = { namespace, data, fitKey };
  const shown = data ? last.current : last.current?.namespace === namespace ? last.current : null;

  useCommands(
    "topology",
    reachable
      ? [
          {
            id: "topology.problems",
            title: problemsOnly ? "Show everything on the map" : "Show only problems on the map",
            group: "This page",
            icon: ErrorIcon,
            keywords: ["filter", "failing", "topology"],
            run: () => setParam("problems", problemsOnly ? "" : "true"),
          },
          ...(namespace && app
            ? [{ id: "topology.all-apps", title: "Show every app on the map", group: "This page", icon: TopologyIcon, run: () => setParam("app", "") }]
            : []),
        ]
      : [],
    [reachable, namespace, problemsOnly, app, searchParams],
  );

  const apps = shown?.data.apps ?? [];
  const where = wholeCluster ? "the cluster" : app ? `app ${app}` : `namespace ${namespace}`;
  // An app from another namespace's link, or one that's gone.
  const unknownApp = Boolean(namespace && shown && app && !apps.some((a) => a.name === app));
  // A namespace's name in the cluster view links to its own map.
  const namespaceMap = (id) => {
    const next = new URLSearchParams(searchParams);
    next.set("ns", id.replace(/^namespaces\//, ""));
    next.delete("app");
    return `/c/${cluster.id}/topology?${next}`;
  };
  const tools = (
    <>
      {!wholeCluster && (
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
      )}
      <Switch checked={problemsOnly} onChange={(on) => setParam("problems", on ? "true" : "")}>
        Problems only
      </Switch>
      {expand.length > 0 && (
        <Button size="sm" variant="quiet" onClick={() => setExpanded({ namespace, ids: [] })}>
          {wholeCluster ? "Fold namespaces up" : "Group all pods"}
        </Button>
      )}
    </>
  );

  return (
    <section className="topology-page">
      <PageHeader title={page.label} description={page.about} />

      {error && !shown && (
        <Callout tone="error" title="Couldn't draw the map" detail={error.detail}>
          {error.message}
        </Callout>
      )}
      {!error && !shown && reachable && <Skeleton height={480} />}

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
          expands={wholeCluster ? "namespaces" : "pods"}
          onExpand={(id, on) =>
            setExpanded({ namespace, ids: on ? [...new Set([...expand, id])] : expand.filter((other) => other !== id) })
          }
          arrange={wholeCluster ? CLUSTER : undefined}
          groupHref={wholeCluster ? namespaceMap : undefined}
        />
      )}
    </section>
  );
}
