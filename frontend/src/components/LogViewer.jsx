import { useEffect, useRef, useState } from "react";
import Button from "./Button.jsx";
import { Select } from "./Field.jsx";
import { Checkbox } from "./Toggle.jsx";
import { PauseIcon, PlayIcon, RefreshIcon } from "./icons.jsx";

const LINE_CHOICES = [100, 500, 2000];
// Keep at most this many lines in the browser while following, so a chatty
// container can't fill up memory.
const MAX_LINES = 5000;

// Shows a pod's logs. `containers` comes from the detail endpoint.
export default function LogViewer({ clusterId, namespace, pod, containers }) {
  // Start with the first normal container (init containers usually finished long ago).
  const [container, setContainer] = useState((containers.find((c) => !c.role) ?? containers[0])?.name ?? "");
  const [lines, setLines] = useState(500);
  const [previous, setPrevious] = useState(false);
  const [follow, setFollow] = useState(false);
  const [reloads, setReloads] = useState(0);

  const [text, setText] = useState("");
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const boxRef = useRef(null);

  // Fetch the logs whenever an option changes. Each run cancels the one before.
  useEffect(() => {
    const controller = new AbortController();
    const params = new URLSearchParams({ container, tail: lines, previous, follow });
    const url = `/api/clusters/${encodeURIComponent(clusterId)}/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(pod)}/logs?${params}`;

    setText("");
    setError(null);
    setLoading(true);

    async function load() {
      try {
        const res = await fetch(url, { signal: controller.signal });
        if (!res.ok) {
          const body = await res.json().catch(() => null);
          throw new Error(body?.error ?? `Request failed (HTTP ${res.status})`);
        }
        // Read the response bit by bit, so followed logs appear as they arrive.
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        setLoading(false);
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          const chunk = decoder.decode(value, { stream: true });
          setText((prev) => keepLastLines(prev + chunk, MAX_LINES));
        }
      } catch (err) {
        if (err.name !== "AbortError") setError(err); // aborting is us, not a failure
      }
      setLoading(false);
    }

    load();
    // Runs when options change or the page closes: stops the request, which
    // also ends the stream on the server.
    return () => controller.abort();
  }, [clusterId, namespace, pod, container, lines, previous, follow, reloads]);

  // While following, keep the newest lines in view.
  useEffect(() => {
    if (follow && boxRef.current) boxRef.current.scrollTop = boxRef.current.scrollHeight;
  }, [text, follow]);

  return (
    <div>
      <div className="log-toolbar">
        <label className="picker">
          <span className="picker-label">Container</span>
          <Select size="sm" value={container} onChange={(e) => setContainer(e.target.value)}>
            {containers.map((c) => (
              <option key={c.name} value={c.name}>
                {c.name}
                {c.role ? ` (${c.role})` : ""}
              </option>
            ))}
          </Select>
        </label>
        <label className="picker">
          <span className="picker-label">Last</span>
          <Select size="sm" value={lines} onChange={(e) => setLines(Number(e.target.value))}>
            {LINE_CHOICES.map((n) => (
              <option key={n} value={n}>
                {n} lines
              </option>
            ))}
          </Select>
        </label>
        <Checkbox checked={previous} onChange={setPrevious} title="Logs from before the container's last restart">
          Previous run
        </Checkbox>
        <div className="table-toolbar-right">
          {!follow && (
            <Button variant="quiet" size="sm" icon={RefreshIcon} onClick={() => setReloads((n) => n + 1)}>
              Refresh
            </Button>
          )}
          <Button
            variant={follow ? "primary" : "secondary"}
            size="sm"
            icon={follow ? PauseIcon : PlayIcon}
            onClick={() => setFollow(!follow)}
            disabled={previous}
            title={previous ? "A previous run has finished, so there's nothing to follow" : undefined}
          >
            {follow ? "Following… (stop)" : "Follow"}
          </Button>
        </div>
      </div>

      {error ? (
        <div className="field-error">{error.message}</div>
      ) : (
        <pre ref={boxRef} className="log-box">
          {text || (loading ? "Loading…" : "No log lines.")}
        </pre>
      )}
    </div>
  );
}

// keepLastLines drops the oldest lines once there are more than max.
function keepLastLines(text, max) {
  const lines = text.split("\n");
  return lines.length > max ? lines.slice(-max).join("\n") : text;
}
