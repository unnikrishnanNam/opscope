import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useLocation, useOutletContext, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import Button from "../components/Button.jsx";
import { Callout, EmptyState } from "../components/Callout.jsx";
import { Field, TextInput } from "../components/Field.jsx";
import { Skeleton } from "../components/Loading.jsx";
import { PageHeader } from "../components/PageHeader.jsx";
import { SegmentedControl } from "../components/Toggle.jsx";
import TopologyView from "../components/TopologyView.jsx";
import { CloseIcon, NamespaceIcon, SearchIcon, TopologyIcon } from "../components/icons.jsx";
import { countOf, kindIcon, kindName } from "../graph/kinds.js";
import { typedNamespace } from "../namespaces.js";
import { GRAPH_KINDS, topologyRequest } from "./topologyRequest.js";
import "./Topology.css";

// Without live updates (see live.js), or for kinds that can't be watched.
const REFRESH_MS = 10_000;

// The Topology page: how objects connect. Three ways in, from the URL:
//
//   ?ns=shop                        a namespace's map (the namespace picker's)
//   ?object=deployments/shop/api    one object and what's around it (&depth=1–3)
//   ?q=pods using db-creds          a graph search, from the command palette
//
// With none (All namespaces), it lists the namespaces to pick from.
export default function Topology({ page }) {
  const { cluster, reachable } = useOutletContext();
  const [params, setParams] = useSearchParams();
  const request = topologyRequest(params);
  const base = reachable ? `/clusters/${cluster.id}` : null;
  const path = base && request.path && `${base}${request.path}`;
  const { data, error } = useApi(path, { refreshMs: REFRESH_MS, live: GRAPH_KINDS });
  // The namespace list, when no namespace is picked: object counts from the name index.
  const names = useApi(base && request.mode === "pick" ? `${base}/names` : null, { refreshMs: REFRESH_MS, live: GRAPH_KINDS });
  // Whether a namespace with an empty map exists: a link can name one this cluster doesn't have.
  const emptyNamespace = request.mode === "namespace" && data && !data.nodes?.length;
  const namespaces = useApi(base && emptyNamespace ? `${base}/namespaces` : null, { live: "namespaces" });

  // A namespace card, the clear button or an empty state's link goes away
  // with the view it led from, taking the keyboard's place with it. Keep
  // that place on the page, so the next Tab goes into the new view rather
  // than back to the top of the document.
  const pageRef = useRef(null);
  const lastKey = useRef(request.key);
  useEffect(() => {
    if (lastKey.current === request.key) return;
    lastKey.current = request.key;
    if (!document.activeElement || document.activeElement === document.body) pageRef.current?.focus({ preventScroll: true });
  });

  // Leaving an object or a search goes back to the namespace's map.
  const clear = () => {
    const next = new URLSearchParams();
    if (params.get("ns")) next.set("ns", params.get("ns"));
    setParams(next);
  };

  return (
    <section className="topology-page" ref={pageRef} tabIndex={-1}>
      <PageHeader title={page.label} description={page.about} />

      {request.mode === "object" && (
        <Scope icon={kindIcon(request.object.resource)} onClear={clear} clearLabel="Back to the namespace's map">
          Around {kindName(request.object.resource)} <strong>{request.object.name}</strong>
          {request.object.namespace && <span className="topology-scope-ns"> in {request.object.namespace}</span>}
          <span className="topology-scope-steps">
            <span aria-hidden="true">Steps away</span>
            <SegmentedControl
              label="Steps away from this object"
              size="sm"
              value={String(request.depth)}
              onChange={(v) => {
                const next = new URLSearchParams(params);
                next.set("depth", v);
                setParams(next, { replace: true });
              }}
              options={["1", "2", "3"].map((v) => ({ value: v, label: v }))}
            />
          </span>
        </Scope>
      )}
      {request.mode === "search" && (
        <Scope icon={SearchIcon} onClear={clear} clearLabel="Clear the search">
          <span className="topology-scope-q">“{request.q}”</span>
          {data && <span className="topology-scope-ns">{data.matches?.length ? countOf(data.matches.length, "match") : "no matches"}</span>}
          {request.namespace && <span className="topology-scope-ns">in {request.namespace}</span>}
        </Scope>
      )}

      {!reachable ? null : request.mode === "pick" ? (
        <NamespacePicker names={names} />
      ) : (
        <MapOf request={request} data={data} error={error} cluster={cluster} path={path} namespaces={namespaces} />
      )}
    </section>
  );
}

// MapOf shows a graph answer, or why there isn't one.
function MapOf({ request, data, error, cluster, path, namespaces }) {
  const answer = useMemo(() => data && { ...data, nodes: data.nodes ?? [], edges: data.edges ?? [] }, [data]);
  if (error && !data) {
    if (error.status === 404 && request.mode === "object") {
      return (
        <Callout title={`Opscope can't find ${kindName(request.object.resource)} ${request.object.name}`} detail={error.message}>
          It may have been deleted, or its kind may not be one the topology holds.
        </Callout>
      );
    }
    if (error.status === 400 && request.mode === "search") {
      return <Callout title="Opscope can't read that search">{error.message}</Callout>;
    }
    if (error.status === 403) {
      return (
        <Callout tone="warning" title="Opscope isn't allowed to read this" detail={error.detail}>
          {error.message}
        </Callout>
      );
    }
    return (
      <Callout tone="error" title="Couldn't draw the map" detail={error.detail}>
        {error.message}
      </Callout>
    );
  }
  if (!answer) return <Skeleton height={480} />;
  if (answer.nodes.length === 0) {
    if (request.mode === "namespace" && !namespaces.data && !namespaces.error) return <Skeleton height={480} />;
    return <Nothing request={request} namespaces={namespaces.data} cluster={cluster} />;
  }

  const label =
    request.mode === "object"
      ? `Map around ${kindName(request.object.resource)} ${request.object.name}`
      : request.mode === "search"
        ? `Map of what matches ${request.q}`
        : `Map of ${request.namespace}`;
  return (
    <>
      {answer.built === "on_request" && (
        <Callout compact>
          This map is read from the cluster when you open it, and again every 10 seconds: Opscope isn't keeping a live copy
          of this cluster.
        </Callout>
      )}
      <TopologyView
        key={request.key}
        answer={answer}
        clusterId={cluster.id}
        label={label}
        fitKey={path}
        level={2}
      />
    </>
  );
}

// Nothing says why there's no map: a search that matched nothing, a
// namespace this cluster doesn't have, or one with nothing the topology
// holds. (A map around an object always has the object.)
function Nothing({ request, namespaces, cluster }) {
  const { pathname } = useLocation();
  if (request.mode === "search") {
    return (
      <EmptyState icon={SearchIcon} title={`Nothing matches “${request.q}”`}>
        Search finds what uses an object, like <Example>pods using db-creds</Example>, or what runs on a node, like{" "}
        <Example>what runs on node-3</Example>.
      </EmptyState>
    );
  }
  if (namespaces && !namespaces.some((n) => n.name === request.namespace)) {
    return (
      <EmptyState
        icon={NamespaceIcon}
        title={`${cluster.name} has no namespace ${request.namespace}`}
        action={<Button to={pathname}>See its namespaces</Button>}
      >
        The link may be from another cluster, or the namespace was deleted.
      </EmptyState>
    );
  }
  return (
    <EmptyState icon={TopologyIcon} title={`Nothing to map in ${request.namespace}`}>
      It has no workloads, pods, Services, routes, config or storage for Opscope to show.
    </EmptyState>
  );
}

// Example is a search to type, kept on one line.
function Example({ children }) {
  return <span className="topology-example">“{children}”</span>;
}

// Scope says what the map is limited to, with a way back.
function Scope({ icon: Icon, onClear, clearLabel, children }) {
  return (
    <div className="topology-scope">
      <Icon size={16} className="topology-scope-icon" />
      <div className="topology-scope-text">{children}</div>
      <button type="button" className="topology-scope-clear" onClick={onClear} title={clearLabel}>
        <CloseIcon size={14} />
        <span className="visually-hidden">{clearLabel}</span>
      </button>
    </div>
  );
}

// withNamespace is the URL of a namespace's map: no object, search or depth.
function withNamespace(params, ns) {
  const next = new URLSearchParams(params);
  next.set("ns", ns);
  next.delete("object");
  next.delete("q");
  next.delete("depth");
  return next;
}

// OpenNamespace asks for a namespace by name, for a user who can't list them.
function OpenNamespace() {
  const [params, setParams] = useSearchParams();
  const [text, setText] = useState("");
  const name = typedNamespace(text);
  const invalid = text.trim() !== "" && !name;
  return (
    <form
      className="topology-open-ns"
      onSubmit={(e) => {
        e.preventDefault();
        if (name) setParams(withNamespace(params, name));
      }}
    >
      <Field label="Namespace" error={invalid ? "A namespace's name has lowercase letters, digits and dashes" : null}>
        <TextInput
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="team-a"
          autoComplete="off"
          spellCheck={false}
        />
      </Field>
      <Button type="submit" variant="primary" disabled={!name}>
        Show its map
      </Button>
    </form>
  );
}

// NamespacePicker lists the cluster's namespaces with how many objects
// each holds, for "All namespaces", where a map of everything would be too
// much to read.
function NamespacePicker({ names }) {
  const [params] = useSearchParams();
  if (names.error && !names.data) {
    return (
      <Callout tone="error" title="Couldn't list the namespaces" detail={names.error.detail}>
        {names.error.message}
      </Callout>
    );
  }
  if (!names.data) return <Skeleton height={160} />;
  const counts = new Map();
  for (const o of names.data.objects ?? []) {
    if (!o.namespace || !GRAPH_KINDS.includes(o.resource)) continue;
    counts.set(o.namespace, (counts.get(o.namespace) ?? 0) + 1);
  }
  const namespaces = [...counts].sort((a, b) => (a[0] < b[0] ? -1 : 1));
  if (namespaces.length === 0) {
    // A user who may read only some namespaces can't list any kind across
    // all of them, so the name index is empty: that's no reason to say
    // there's nothing to map. Let them say which namespace.
    if (names.data.skipped?.some((s) => s.code === "forbidden")) {
      return (
        <EmptyState
          icon={TopologyIcon}
          title="Opscope can't list objects across all namespaces"
          action={<OpenNamespace />}
        >
          It may only be allowed to read some namespaces. Enter one to see its map, or type it in the namespace picker
          above.
        </EmptyState>
      );
    }
    return (
      <EmptyState icon={TopologyIcon} title="Nothing to map yet">
        No namespace holds objects Opscope can read.
      </EmptyState>
    );
  }
  const linkTo = (ns) => `?${withNamespace(params, ns)}`;
  return (
    <div className="topology-pick">
      <p className="topology-pick-intro">
        Pick a namespace to see its map, or choose one in the namespace picker above. To find what uses an object, search
        from the command palette: <Example>pods using db-creds</Example>.
      </p>
      <ul className="topology-pick-list">
        {namespaces.map(([ns, n]) => (
          <li key={ns}>
            <Link to={linkTo(ns)} className="topology-pick-card">
              <NamespaceIcon size={16} className="topology-pick-icon" />
              <span className="topology-pick-text">
                <span className="topology-pick-name">{ns}</span>
                <span className="topology-pick-count">{countOf(n, "object")}</span>
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}
