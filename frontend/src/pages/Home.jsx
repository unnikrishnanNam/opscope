import { Navigate } from "react-router";
import { useClusters } from "../clusters.jsx";
import { Callout } from "../components/Callout.jsx";
import { Spinner } from "../components/Loading.jsx";
import Welcome from "./Welcome.jsx";

// "/" has no page of its own: open the first cluster, or welcome a new user.
export default function Home() {
  const { data: clusters, error } = useClusters();

  if (error) {
    return (
      <Callout tone="error" title="Couldn't load clusters">
        {error.message}
      </Callout>
    );
  }
  if (!clusters) {
    return (
      <div className="shell-loading">
        <Spinner size={20} label="Loading clusters" />
      </div>
    );
  }
  if (clusters.length === 0) return <Welcome />;
  return <Navigate to={`/c/${clusters[0].id}/overview`} replace />;
}
