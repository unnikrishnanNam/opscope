import { useEffect, useState } from "react";

const CHECK_EVERY_MS = 15_000;

// Calls /api/health on load and every 15 seconds, and shows whether the
// backend answered.
export default function HealthBadge() {
  // "checking" until the first answer, then "ok" or "down".
  const [state, setState] = useState("checking");
  const [version, setVersion] = useState("");

  useEffect(() => {
    async function check() {
      try {
        const res = await fetch("/api/health");
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const body = await res.json();
        setVersion(body.version);
        setState("ok");
      } catch {
        setState("down");
      }
    }

    check();
    const timer = setInterval(check, CHECK_EVERY_MS);
    return () => clearInterval(timer); // stop when the component goes away
  }, []);

  const label = {
    checking: "Checking API",
    ok: `API ok · ${version}`,
    down: "API unreachable",
  }[state];

  return (
    <span className={`badge badge-${state}`}>
      <span className="dot" />
      {label}
    </span>
  );
}
