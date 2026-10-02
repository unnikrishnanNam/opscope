import { Navigate } from "react-router";
import { useClusters } from "../clusters.jsx";
import ErrorBox from "../components/ErrorBox.jsx";

// "/" has no page of its own: open the first cluster, or ask for one.
export default function Home() {
  const { data: clusters, error } = useClusters();

  if (error) return <ErrorBox title="Couldn't load clusters" message={error.message} />;
  if (!clusters) return <p className="muted">Loading…</p>;
  if (clusters.length === 0) return <Navigate to="/clusters/add" replace />;
  return <Navigate to={`/c/${clusters[0].id}/overview`} replace />;
}
