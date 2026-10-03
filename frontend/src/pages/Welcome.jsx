import Button from "../components/Button.jsx";
import CodeBlock from "../components/CodeBlock.jsx";
import { Logo } from "../components/Logo.jsx";
import { PlusIcon } from "../components/icons.jsx";
import "./Clusters.css";

// The first screen, before any cluster is known: what Opscope is, and the
// ways to give it a cluster.
export default function Welcome() {
  return (
    <section className="welcome">
      <Logo height={44} />
      <h1 className="welcome-title">Connect a cluster to get started</h1>
      <p className="welcome-lead">
        Opscope is a small, read-only Kubernetes dashboard: nodes, workloads, config, networking, logs and live usage.
        It never changes anything in a cluster.
      </p>

      <div className="welcome-options">
        <div className="welcome-option">
          <h2>Add one here</h2>
          <p>
            Paste or upload a kubeconfig and pick a context. Opscope checks that it can connect, then saves only that
            context.
          </p>
          <div className="welcome-action">
            <Button to="/clusters/add" variant="primary" icon={PlusIcon}>
              Add a cluster
            </Button>
          </div>
        </div>

        <div className="welcome-option">
          <h2>Or start Opscope with one</h2>
          <p>Point it at a kubeconfig file when it starts. Any kubeconfig works, including cloud login plugins:</p>
          <CodeBlock code="OPSCOPE_KUBECONFIG=/path/to/kubeconfig" lineNumbers={false} />
          <p>
            Running as a pod? Set <code>OPSCOPE_IN_CLUSTER=true</code> to use its service account. The README covers
            Docker and Kubernetes.
          </p>
        </div>
      </div>
    </section>
  );
}
