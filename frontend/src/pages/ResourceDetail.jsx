import { Link, useLocation, useOutletContext, useParams, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { age } from "../format.js";
import { detailPath, nsQuery } from "../sections.js";
import ErrorBox from "../components/ErrorBox.jsx";
import StatusBadge from "../components/StatusBadge.jsx";
import EventsTable from "../components/EventsTable.jsx";
import SecretKeys from "../components/SecretKeys.jsx";
import LogViewer from "../components/LogViewer.jsx";

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
  const path = reachable
    ? `/clusters/${cluster.id}/${page.resource}/${parts.map(encodeURIComponent).join("/")}`
    : null;
  const { data: d, error } = useApi(path, { refreshMs: REFRESH_MS });

  function openTab(next) {
    const params = new URLSearchParams(searchParams);
    if (next === "summary") params.delete("tab");
    else params.set("tab", next);
    setSearchParams(params);
  }

  const tabs = [
    ["summary", "Summary"],
    ["yaml", "YAML"],
    ["events", d ? `Events (${d.events.length})` : "Events"],
  ];
  if (page.resource === "pods") tabs.push(["logs", "Logs"]);

  const status = d?.fields.find((f) => f.label === "Status")?.value;

  return (
    <section>
      <div className="detail-header">
        <span className="tag">{d?.kind ?? page.label}</span>
        <h1 className="page-title">{name}</h1>
        {status && <StatusBadge status={status} />}
      </div>
      <p className="page-about">
        {namespace && (
          <>
            Namespace <strong>{namespace}</strong> ·{" "}
          </>
        )}
        {d ? `created ${age(d.created)} ago` : " "}
      </p>

      {error && <ErrorBox title={`Couldn't load ${name}`} message={error.message} detail={error.detail} />}

      <div className="tabs" role="tablist">
        {tabs.map(([key, label]) => (
          <button
            key={key}
            type="button"
            role="tab"
            aria-selected={tab === key}
            className={`tab ${tab === key ? "tab-active" : ""}`}
            onClick={() => openTab(key)}
          >
            {label}
          </button>
        ))}
      </div>

      {d && tab === "summary" && <Summary d={d} cluster={cluster} resource={page.resource} />}
      {d && tab === "yaml" && <YamlView d={d} isSecret={page.resource === "secrets"} />}
      {d && tab === "events" && (
        <EventsTable clusterId={cluster.id} events={d.events} showObject={false} emptyText="No recent events for this object." />
      )}
      {d && tab === "logs" && (
        <LogViewer clusterId={cluster.id} namespace={namespace} pod={name} containers={d.containers} />
      )}
    </section>
  );
}

function Summary({ d, cluster, resource }) {
  const search = nsQuery(useLocation().search);

  return (
    <div className="detail-sections">
      <dl className="facts">
        {d.fields
          .filter((f) => f.label !== "Status") // already in the header
          .map((f) => (
            <Fact key={f.label} label={f.label}>
              {f.value}
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
      </dl>

      {resource === "secrets" && (
        <Section title="Keys">
          <SecretKeys cluster={cluster} secret={{ namespace: d.namespace, name: d.name, keys: d.secretKeys }} />
        </Section>
      )}

      {d.containers.length > 0 && (
        <Section title="Containers">
          <Containers containers={d.containers} />
        </Section>
      )}

      {d.data.length > 0 && (
        <Section title="Data">
          {d.data.map((entry) => (
            <div key={entry.key} className="data-entry">
              <div className="data-key">
                <span className="mono">{entry.key}</span>
                <span className="muted">{entry.size} bytes</span>
              </div>
              {entry.binary ? (
                <p className="muted">Binary data, not shown.</p>
              ) : (
                <pre className="code-box">{entry.value}</pre>
              )}
            </div>
          ))}
        </Section>
      )}

      {d.tables.map((t) => (
        <Section key={t.title} title={t.title}>
          <SimpleTable columns={t.columns} rows={t.rows} />
        </Section>
      ))}

      {d.conditions.length > 0 && (
        <Section title="Conditions">
          <SimpleTable
            columns={["Type", "Status", "Reason", "Message", "Changed"]}
            rows={d.conditions.map((c) => [
              c.type,
              c.status,
              c.reason,
              c.message,
              c.lastTransition ? `${age(c.lastTransition)} ago` : "",
            ])}
          />
        </Section>
      )}

      <Section title="Labels">
        <KeyValues values={d.labels} empty="No labels." />
      </Section>
      <Section title="Annotations">
        <KeyValues values={d.annotations} empty="No annotations." />
      </Section>
    </div>
  );
}

function Section({ title, children }) {
  return (
    <div className="detail-section">
      <h2 className="card-title">{title}</h2>
      {children}
    </div>
  );
}

function Fact({ label, children }) {
  return (
    <div className="fact">
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

// Containers with their live state (for pods) or just their spec (for templates).
function Containers({ containers }) {
  const live = containers.some((c) => c.ready !== null);
  return (
    <div className="card-scroll">
      <table className="table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Image</th>
            {live && <th>State</th>}
            {live && <th className="num">Restarts</th>}
            <th>Ports</th>
            <th>Requests</th>
            <th>Limits</th>
          </tr>
        </thead>
        <tbody>
          {containers.map((c) => (
            <tr key={c.name}>
              <td className="name">
                {c.name}
                {c.role && <span className="tag tag-gap">{c.role}</span>}
              </td>
              <td className="mono">{c.image}</td>
              {live && (
                <td>
                  <StatusBadge status={c.stateReason || c.state} />
                  {c.lastExit && <div className="muted small">Last exit: {c.lastExit}</div>}
                </td>
              )}
              {live && <td className="num">{c.restarts}</td>}
              <td className="mono">{c.ports.join(", ") || <span className="muted">–</span>}</td>
              <td className="mono">{c.requests || <span className="muted">–</span>}</td>
              <td className="mono">{c.limits || <span className="muted">–</span>}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function SimpleTable({ columns, rows }) {
  return (
    <div className="card-scroll">
      <table className="table simple-table">
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c}>{c}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr key={i}>
              {row.map((cell, j) => (
                <td key={j}>{cell || <span className="muted">–</span>}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function KeyValues({ values, empty }) {
  const entries = Object.entries(values).sort(([a], [b]) => a.localeCompare(b));
  if (entries.length === 0) return <p className="muted">{empty}</p>;
  return (
    <dl className="key-values">
      {entries.map(([k, v]) => (
        <div key={k}>
          <dt className="mono">{k}</dt>
          <dd className="mono clamp-3" title={v}>
            {v || <span className="muted">(empty)</span>}
          </dd>
        </div>
      ))}
    </dl>
  );
}

function YamlView({ d, isSecret }) {
  async function copy() {
    try {
      await navigator.clipboard.writeText(d.yaml);
    } catch {
      // Copying can be blocked by the browser; the text is still selectable.
    }
  }
  return (
    <div>
      <div className="table-toolbar">
        <span className="muted">
          Read-only. managedFields are left out{isSecret ? "; secret values are hidden" : ""}.
        </span>
        <div className="table-toolbar-right">
          <button type="button" className="button button-quiet" onClick={copy}>
            Copy
          </button>
        </div>
      </div>
      <pre className="code-box yaml-box">{d.yaml}</pre>
    </div>
  );
}
