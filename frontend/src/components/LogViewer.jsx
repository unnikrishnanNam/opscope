import { useEffect, useState } from "react";
import { copyText } from "../clipboard.js";
import { useCommands } from "../commands/registry.jsx";
import { kubectlLogs } from "../kubectl.js";
import Button from "./Button.jsx";
import { Callout } from "./Callout.jsx";
import { Select } from "./Field.jsx";
import LogView from "./LogView.jsx";
import { Checkbox, Switch } from "./Toggle.jsx";
import { CopyIcon, LogsIcon, PauseIcon, PlayIcon, RefreshIcon, WrapTextIcon } from "./icons.jsx";
import "./LogViewer.css";

const LINE_CHOICES = [100, 500, 2000];
// Keep at most this many lines in the browser while following, so a chatty
// container can't fill up memory.
const MAX_LINES = 5000;

// A pod's logs: which container, how many lines, the previous run, line
// wrapping and following, above a LogView. `containers` comes from the
// detail endpoint.
export default function LogViewer({ clusterId, namespace, pod, containers }) {
  // Start with the first normal container (init containers usually finished long ago).
  const [container, setContainer] = useState((containers.find((c) => !c.role) ?? containers[0])?.name ?? "");
  const [lines, setLines] = useState(500);
  const [previous, setPrevious] = useState(false);
  const [follow, setFollow] = useState(false);
  const [wrap, setWrap] = useState(true);
  const [reloads, setReloads] = useState(0);

  const [text, setText] = useState("");
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);

  // The same controls in the command palette, while the logs are on screen.
  useCommands(
    "logs",
    logCommands({ namespace, pod, containers, container, setContainer, previous, setPrevious, follow, setFollow, wrap, setWrap, setReloads }),
    [namespace, pod, containers, container, previous, follow, wrap],
  );

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

  return (
    <div className="log-viewer">
      <div className="log-toolbar">
        {containers.length > 1 ? (
          <Select size="sm" aria-label="Container" value={container} onChange={(e) => setContainer(e.target.value)}>
            {containers.map((c) => (
              <option key={c.name} value={c.name}>
                {c.name}
                {c.role ? ` (${c.role})` : ""}
              </option>
            ))}
          </Select>
        ) : (
          <span className="log-container" title="Container">
            {container}
          </span>
        )}
        <Select size="sm" aria-label="Lines" value={lines} onChange={(e) => setLines(Number(e.target.value))}>
          {LINE_CHOICES.map((n) => (
            <option key={n} value={n}>
              Last {n} lines
            </option>
          ))}
        </Select>
        <Checkbox
          checked={previous}
          // A finished run has nothing to follow.
          onChange={(on) => {
            setPrevious(on);
            if (on) setFollow(false);
          }}
          title="Logs from before the container's last restart"
        >
          Previous run
        </Checkbox>
        <div className="log-toolbar-end">
          <Switch checked={wrap} onChange={setWrap}>
            Wrap lines
          </Switch>
          <Switch
            checked={follow}
            onChange={setFollow}
            disabled={previous}
            title={
              previous ? "A previous run has finished, so there's nothing to follow" : "Show new lines as they arrive"
            }
          >
            Follow
          </Switch>
          {!follow && (
            <Button variant="quiet" size="sm" icon={RefreshIcon} onClick={() => setReloads((n) => n + 1)}>
              Refresh
            </Button>
          )}
        </div>
      </div>

      {error ? (
        <Callout tone="error" title="Couldn't load the logs">
          {error.message}
        </Callout>
      ) : (
        <LogView text={text} loading={loading} live={follow} wrap={wrap} height="calc(100vh - 330px)" />
      )}
    </div>
  );
}

// keepLastLines drops the oldest lines once there are more than max.
function keepLastLines(text, max) {
  const lines = text.split("\n");
  return lines.length > max ? lines.slice(-max).join("\n") : text;
}

// The log viewer's commands for the palette: what its toolbar does, and a
// kubectl line for the logs on screen.
function logCommands(o) {
  const group = "Logs";
  const commands = [];
  if (!o.previous) {
    commands.push({
      id: "logs.follow",
      title: o.follow ? "Stop following logs" : "Follow logs",
      group,
      icon: o.follow ? PauseIcon : PlayIcon,
      run: () => o.setFollow(!o.follow),
    });
  }
  commands.push(
    {
      id: "logs.previous",
      title: o.previous ? "Show the current run's logs" : "Show the previous run's logs",
      group,
      icon: LogsIcon,
      keywords: ["restart", "crash"],
      run: () => {
        o.setPrevious(!o.previous);
        if (!o.previous) o.setFollow(false); // a finished run has nothing to follow
      },
    },
    {
      id: "logs.wrap",
      title: o.wrap ? "Don't wrap log lines" : "Wrap log lines",
      group,
      icon: WrapTextIcon,
      run: () => o.setWrap(!o.wrap),
    },
  );
  if (!o.follow) {
    commands.push({ id: "logs.reload", title: "Reload logs", group, icon: RefreshIcon, run: () => o.setReloads((n) => n + 1) });
  }
  if (o.containers.length > 1) {
    commands.push({
      id: "logs.container",
      title: "Show another container's logs…",
      detail: o.container,
      group,
      icon: LogsIcon,
      keywords: ["container"],
      items: () =>
        o.containers.map((c) => ({
          id: `logs.container.${c.name}`,
          title: c.name,
          detail: c.name === o.container ? "shown" : c.role,
          group: "Containers",
          run: () => o.setContainer(c.name),
        })),
    });
  }
  const line = kubectlLogs(o.namespace, o.pod, { container: o.container, previous: o.previous });
  commands.push({
    id: "logs.kubectl",
    title: "Copy kubectl logs",
    detail: line,
    group,
    icon: CopyIcon,
    keywords: ["command"],
    run: () => copyText(line, "the kubectl command"),
  });
  return commands;
}
