import { useApi } from "../api.js";

const CHECK_EVERY_MS = 15_000;

// Shows whether the OpScope backend answers, and its version.
// (This is about OpScope itself, not about a cluster.)
export default function HealthBadge() {
  const { data, error } = useApi("/health", { refreshMs: CHECK_EVERY_MS });

  let state = "checking";
  let label = "Checking server";
  if (error) {
    state = "down";
    label = "Server unreachable";
  } else if (data) {
    state = "ok";
    label = `OpScope ${data.version}`;
  }

  return (
    <div className={`health health-${state}`}>
      <span className="dot" />
      {label}
    </div>
  );
}
