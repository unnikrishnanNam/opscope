import { useApi } from "../api.js";

const CHECK_EVERY_MS = 15_000;

// Shows whether the Opscope backend answers, and its version.
// (This is about Opscope itself, not about a cluster.)
export default function HealthBadge() {
  const { data, error } = useApi("/health", { refreshMs: CHECK_EVERY_MS });

  let state = "checking";
  let label = "Checking server";
  if (error) {
    state = "down";
    label = "Server unreachable";
  } else if (data) {
    state = "ok";
    label = `Opscope ${data.version}`;
  }

  return (
    <div className={`health health-${state}`}>
      <span className="dot" />
      {label}
    </div>
  );
}
