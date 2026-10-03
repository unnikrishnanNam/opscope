import { Link, useLocation, useOutletContext, useParams, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { age } from "../format.js";
import { detailPath, nsQuery } from "../sections.js";
import { Callout } from "../components/Callout.jsx";
import { Card, Section } from "../components/Card.jsx";
import CodeBlock from "../components/CodeBlock.jsx";
import { SimpleTable } from "../components/DataTable.jsx";
import EventList from "../components/EventList.jsx";
import { Fact, FactGrid, KeyValueList } from "../components/Facts.jsx";
import { Skeleton, SkeletonText } from "../components/Loading.jsx";
import LogViewer from "../components/LogViewer.jsx";
import { PageHeader } from "../components/PageHeader.jsx";
import SecretKeys from "../components/SecretKeys.jsx";
import StatusBadge from "../components/StatusBadge.jsx";
import { Tag } from "../components/Tag.jsx";
import Tabs from "../components/Tabs.jsx";
import "./ResourceDetail.css";

const REFRESH_MS = 10_000;

// The page for one object, e.g. /c/lab/workloads/pods/web/api-1.
// The backend decides which sections apply; this page shows whichever are
// filled in. The open tab is kept in the URL as ?tab=yaml, so links can point at it.
export default function ResourceDetail({ page }) {
  const { cluster, reachable } = useOutletContext();
  const { namespace = "", name } = useParams(); // no :namespace for cluster-wide kinds
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = searchParams.get("tab") ?? "summary";

  const parts = page.clusterScoped ? [name] : [namespace, name];
  const path = reachable ? `/clusters/${cluster.id}/${page.resource}/${parts.map(encodeURIComponent).join("/")}` : null;
  const { data: d, error } = useApi(path, { refreshMs: REFRESH_MS });

  function openTab(next) {
    const params = new URLSearchParams(searchParams);
    if (next === "summary") params.delete("tab");
    else params.set("tab", next);
    setSearchParams(params);
  }

  const tabs = [
    { value: "summary", label: "Summary" },
    { value: "yaml", label: "YAML" },
    { value: "events", label: "Events", count: d?.events.length },
  ];
  if (page.resource === "pods") tabs.push({ value: "logs", label: "Logs" });

  const status = d?.fields.find((f) => f.label === "Status")?.value;

  return (
    <section className="detail-page">
      <PageHeader
        before={<Tag>{d?.kind ?? page.label}</Tag>}
        title={name}
        after={status && <StatusBadge status={status} />}
        description={
          <>
            {namespace && (
              <>
                Namespace{" "}
                <Link
                  className="detail-namespace"
                  to={`/c/${cluster.id}/${page.path}?ns=${encodeURIComponent(namespace)}`}
                >
                  {namespace}
                </Link>
                {" · "}
              </>
            )}
            {d && <span title={new Date(d.created).toLocaleString()}>created {age(d.created)} ago</span>}
          </>
        }
      />

      {error && (
        <Callout tone="error" title={`Couldn't load ${name}`} detail={error.detail}>
          {error.message}
        </Callout>
      )}

      {!d && !error && reachable && <DetailSkeleton />}

      {d && (
        <Tabs label={`${d.kind} details`} value={tab} onChange={openTab} tabs={tabs}>
          {tab === "summary" && <Summary d={d} cluster={cluster} resource={page.resource} />}
          {tab === "yaml" && (
            <CodeBlock
              code={d.yaml}
              language="yaml"
              title={`Read-only. managedFields are left out${page.resource === "secrets" ? "; secret values are hidden" : ""}.`}
            />
          )}
          {tab === "events" && (
            <EventList
              clusterId={cluster.id}
              events={d.events}
              showObject={false}
              emptyText="No recent events for this object."
            />
          )}
          {tab === "logs" && (
            <LogViewer clusterId={cluster.id} namespace={namespace} pod={name} containers={d.containers} />
          )}
        </Tabs>
      )}
    </section>
  );
}

function DetailSkeleton() {
  return (
    <div className="detail-sections" aria-hidden="true">
      <Skeleton height={40} />
      <div className="detail-skeleton-card">
        <SkeletonText lines={4} />
      </div>
    </div>
  );
}

function Summary({ d, cluster, resource }) {
  const search = nsQuery(useLocation().search);
  const facts = d.fields.filter((f) => f.label !== "Status"); // already in the header

  return (
    <div className="detail-sections">
      {(facts.length > 0 || d.owners.length > 0) && (
        <Card>
          <FactGrid>
            {facts.map((f) => (
              <Fact key={f.label} label={f.label}>
                {f.value || <span className="cell-empty">–</span>}
              </Fact>
            ))}
            {d.owners.map((o) => {
              const link = o.resource && detailPath(cluster.id, o.resource, d.namespace, o.name);
              return (
                <Fact key={`${o.kind}/${o.name}`} label="Owned by">
                  {link ? <Link to={link + search}>{`${o.kind}/${o.name}`}</Link> : `${o.kind}/${o.name}`}
                </Fact>
              );
            })}
          </FactGrid>
        </Card>
      )}

      {resource === "secrets" && (
        <Section title="Keys" aside={d.secretKeys.length || undefined}>
          <SecretKeys cluster={cluster} secret={{ namespace: d.namespace, name: d.name, keys: d.secretKeys }} />
        </Section>
      )}

      {d.containers.length > 0 && (
        <Section title="Containers" aside={d.containers.length}>
          <Containers containers={d.containers} />
        </Section>
      )}

      {d.data.length > 0 && (
        <Section title="Data" aside={d.data.length}>
          <div className="detail-data">
            {d.data.map((entry) =>
              entry.binary ? (
                <div key={entry.key} className="detail-binary">
                  <span className="mono">{entry.key}</span>
                  <span>Binary data, {entry.size} bytes, not shown.</span>
                </div>
              ) : (
                <CodeBlock
                  key={entry.key}
                  code={entry.value}
                  title={
                    <>
                      <span className="mono detail-data-key">{entry.key}</span> · {entry.size} bytes
                    </>
                  }
                  maxHeight="320px"
                />
              ),
            )}
          </div>
        </Section>
      )}

      {d.tables.map((t) => (
        <Section key={t.title} title={t.title}>
          <SimpleTable label={t.title} columns={t.columns.map((label) => ({ label }))} rows={t.rows} />
        </Section>
      ))}

      {d.conditions.length > 0 && (
        <Section title="Conditions">
          <SimpleTable
            label="Conditions"
            columns={[
              { label: "Type", className: "nowrap" },
              { label: "Status", className: "nowrap" },
              { label: "Reason", className: "nowrap" },
              { label: "Message" },
              { label: "Changed", className: "nowrap" },
            ]}
            rows={d.conditions.map((c) => [
              <strong>{c.type}</strong>,
              c.status,
              c.reason,
              c.message,
              c.lastTransition ? `${age(c.lastTransition)} ago` : "",
            ])}
          />
        </Section>
      )}

      <Section title="Labels" aside={Object.keys(d.labels).length || undefined}>
        <KeyValueList values={d.labels} empty="No labels." />
      </Section>
      <Section title="Annotations" aside={Object.keys(d.annotations).length || undefined}>
        <KeyValueList values={d.annotations} empty="No annotations." />
      </Section>
    </div>
  );
}

// Containers with their live state (for pods) or just their spec (for templates).
function Containers({ containers }) {
  const live = containers.some((c) => c.ready !== null);
  const columns = [
    { label: "Name", className: "nowrap" },
    { label: "Image", className: "mono image" },
    ...(live ? [{ label: "State" }, { label: "Restarts", className: "num" }] : []),
    { label: "Ports", className: "mono" },
    { label: "Requests", className: "mono" },
    { label: "Limits", className: "mono" },
  ];
  const rows = containers.map((c) => [
    <span className="container-name">
      {c.name}
      {c.role && <Tag>{c.role}</Tag>}
    </span>,
    <BreakAt text={c.image} />,
    ...(live
      ? [
          <>
            <StatusBadge status={c.stateReason || c.state} />
            {c.lastExit && <div className="container-last-exit">Last exit: {c.lastExit}</div>}
          </>,
          c.restarts,
        ]
      : []),
    c.ports.join(", "),
    c.requests,
    c.limits,
  ]);
  return <SimpleTable label="Containers" columns={columns} rows={rows} />;
}

// A long image name may wrap only after "/", ":" or "@", so
// "quay.io/argoproj/argocd:v2.13.0" never splits inside "v2.13.0".
function BreakAt({ text }) {
  return text.split(/(?<=[/:@])/).map((part, i) => (
    <span key={i}>
      {i > 0 && <wbr />}
      {part}
    </span>
  ));
}
