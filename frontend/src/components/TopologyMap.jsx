import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { useApi } from "../api.js";
import { age } from "../format.js";
import { indexFindings } from "../graph/trace.js";
import { Callout } from "./Callout.jsx";
import { Skeleton } from "./Loading.jsx";
import TopologyGraph from "./TopologyGraph.jsx";
import TopologyList from "./TopologyList.jsx";
import TopologyPanel from "./TopologyPanel.jsx";
import { SegmentedControl } from "./Toggle.jsx";
import { ListIcon, MapIcon } from "./icons.jsx";
import "./TopologyMap.css";

const REFRESH_MS = 10_000;
const PANEL_W = 360;
const NARROW = "(max-width: 767px)";
const MIN_HEIGHT = 360;
const BELOW = 32 + 30; // the page's bottom padding, and the legend under the map

// TopologyMap is the Map tab: one object's neighbourhood, refreshed every
// 10 seconds, as a map or as a list.
//   objectId  "deployments/shop/web", as in the topology endpoint
export default function TopologyMap({ clusterId, objectId, namespace, label }) {
  const [depth, setDepth] = useState(2);
  const path = `/clusters/${clusterId}/topology?focus=${encodeURIComponent(objectId)}&depth=${depth}`;
  const { data, error } = useApi(path, { refreshMs: REFRESH_MS });

  const steps = (
    <div className="topo-toolbar-group">
      <span className="topo-toolbar-label" aria-hidden="true">
        Steps away
      </span>
      <SegmentedControl
        label="Steps away from this object"
        size="sm"
        value={String(depth)}
        onChange={(v) => setDepth(Number(v))}
        options={["1", "2", "3"].map((v) => ({ value: v, label: v }))}
      />
    </div>
  );

  if (error && !data) {
    return (
      <Callout tone="error" title="Couldn't draw the map" detail={error.detail}>
        {error.message}
      </Callout>
    );
  }
  if (!data) return <Skeleton height={480} />;
  return (
    <TopologyView
      clusterId={clusterId}
      view={data}
      focus={objectId}
      namespace={namespace}
      label={label}
      fitKey={`${objectId}|${depth}`}
      tools={steps}
    />
  );
}

// TopologyView shows a view as a map with a panel for the selected card,
// or as a list; on a phone it starts as a list.
//   tools     more controls for the toolbar
//   empty     what to say when the view is empty
//   expanded  workloads whose pods are shown one by one, and onExpand(id,
//             on) to change that; without it, groups stay grouped
export function TopologyView({ clusterId, view, focus, namespace, label, fitKey, tools, height, empty, expanded, onExpand }) {
  const [mode, setMode] = useState(() => (window.matchMedia?.(NARROW).matches ? "list" : "map"));
  const [selected, setSelected] = useState(null);
  const graph = useRef(null);
  const frame = useRef(null);
  const filled = useFillHeight(frame, !height && mode === "map");
  const index = useMemo(() => indexFindings(view), [view]);
  const shown = selected && view.nodes.some((n) => n.id === selected) ? selected : null;
  const taken = view.snapshot?.taken;

  function close() {
    const id = shown;
    setSelected(null);
    graph.current?.focusCard(id);
  }

  return (
    <div className="topo-view">
      <div className="topo-toolbar">
        <SegmentedControl
          label="Show as"
          size="sm"
          value={mode}
          onChange={setMode}
          options={[
            { value: "map", label: "Map", icon: MapIcon },
            { value: "list", label: "List", icon: ListIcon },
          ]}
        />
        {tools}
        <span className="topo-toolbar-about">
          {view.nodes.length === 1 ? "1 object" : `${view.nodes.length} objects`}
          {taken && (
            <>
              {" · "}
              <time dateTime={taken} title={new Date(taken).toLocaleString()}>
                as of {age(taken)} ago
              </time>
            </>
          )}
        </span>
      </div>

      {mode === "map" ? (
        <>
          <div ref={frame} className="topo-frame" style={{ height: height ?? filled }}>
            <TopologyGraph
              ref={graph}
              view={view}
              focus={focus}
              namespace={namespace}
              selected={shown}
              onSelect={setSelected}
              label={label}
              inset={shown ? PANEL_W : 0}
              fitKey={fitKey}
              empty={empty}
            />
            {shown && (
              <TopologyPanel
                clusterId={clusterId}
                view={view}
                index={index}
                id={shown}
                focus={focus}
                namespace={namespace}
                onSelect={(id) => {
                  setSelected(id);
                  graph.current?.showCard(id);
                }}
                onClose={close}
                expanded={expanded}
                onExpand={
                  onExpand &&
                  ((id, on) => {
                    // The group's card goes; its workload's stays, to group them again.
                    setSelected(id);
                    onExpand(id, on);
                  })
                }
              />
            )}
          </div>
          <Legend />
        </>
      ) : (
        <TopologyList clusterId={clusterId} view={view} focus={focus} namespace={namespace} empty={empty} />
      )}
    </div>
  );
}

// useFillHeight is how tall the map can be to fill the rest of the screen
// below where it starts, with the page scrolled to the top. It's measured
// when the map appears and when the window changes size.
function useFillHeight(ref, enabled) {
  const [height, setHeight] = useState(undefined);
  useLayoutEffect(() => {
    if (!enabled) return;
    function measure() {
      const el = ref.current;
      if (!el) return;
      let box = el.parentElement;
      while (box && !/(auto|scroll)/.test(getComputedStyle(box).overflowY)) box = box.parentElement;
      const top = box
        ? el.getBoundingClientRect().top - box.getBoundingClientRect().top + box.scrollTop
        : el.getBoundingClientRect().top + window.scrollY;
      const room = box ? box.clientHeight : window.innerHeight;
      setHeight(Math.max(MIN_HEIGHT, Math.floor(room - top - BELOW)));
    }
    measure();
    window.addEventListener("resize", measure);
    return () => window.removeEventListener("resize", measure);
  }, [ref, enabled]);
  return height;
}

// Legend: what the lines mean.
function Legend() {
  const line = (className) => (
    <svg width="28" height="8" aria-hidden="true" focusable="false">
      <path d="M 1 4 L 27 4" className={className} />
    </svg>
  );
  return (
    <ul className="topo-legend" aria-label="Legend">
      <li>{line("topo-edge topo-edge-owns")}owns, routes to, attaches</li>
      <li>{line("topo-edge topo-edge-uses")}uses</li>
      <li>{line("topo-edge topo-edge-selects")}selects, runs on</li>
      <li>{line("topo-edge topo-edge-blames")}held up by</li>
      <li className="topo-chain topo-chain-failing">{line("topo-chain-line")}the way to the root cause</li>
    </ul>
  );
}
