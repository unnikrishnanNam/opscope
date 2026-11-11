import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { LINE_STYLES, countOf, kindName, pluralKind } from "../graph/kinds.js";
import { shape, unreadable as unreadableKinds } from "../graph/view.js";
import TopologyGraph from "./TopologyGraph.jsx";
import TopologyList from "./TopologyList.jsx";
import TopologyPanel from "./TopologyPanel.jsx";
import { SegmentedControl } from "./Toggle.jsx";
import { EyeOffIcon, ListIcon, MapIcon, MissingIcon, PodsIcon, UnlinkIcon } from "./icons.jsx";
import "./TopologyView.css";

const PANEL_W = 360;
const NARROW = "(max-width: 767px)";
const MIN_HEIGHT = 360;
const BELOW = 32 + 40; // the page's bottom padding, and the legend under the map

// TopologyView shows a graph answer (GET /api/clusters/{id}/graph/…) as a
// map with a panel for the selected card, or as a list. On a phone it
// starts as a list.
//
//   answer     the endpoint's answer: { nodes, edges, focus, built, partial }
//   clusterId  for links to objects' pages
//   label      names the map for screen readers ("Map of shop")
//   height     the map's height in px; without it, it fills the screen below
//   fitKey     the map is fitted on screen again when this changes
//   empty      what to say when there's nothing to show
//   level      the level of its headings: 3 under a section's h2, 2 when
//              the map is the page's main content
//   initial    { mode, selected } to start with ("map" or "list"; a node ID),
//              for /kit
export default function TopologyView({ answer, clusterId, label, height, fitKey, empty, level = 3, initial }) {
  const [mode, setMode] = useState(() => initial?.mode ?? (window.matchMedia?.(NARROW).matches ? "list" : "map"));
  const [selected, setSelected] = useState(initial?.selected ?? null);
  const [expanded, setExpanded] = useState(() => new Set());
  const view = useMemo(() => shape(answer, { expanded }), [answer, expanded]);
  const graph = useRef(null);
  const frame = useRef(null);
  const filled = useFillHeight(frame, !height && mode === "map");
  const shown = selected && view.nodes.some((n) => n.id === selected) ? selected : null;
  const unreadable = unreadableKinds(answer.partial);

  function close() {
    const id = shown;
    setSelected(null);
    graph.current?.focusCard(id);
  }

  function expand(owner, on) {
    setExpanded((old) => {
      const next = new Set(old);
      if (on) next.add(owner);
      else next.delete(owner);
      return next;
    });
    // Showing each pod: the workload stays selected, to group them again.
    // Grouping them: the group is.
    setSelected(on ? owner : `pods:${owner}`);
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
        <span className="topo-toolbar-about">
          {countOf(view.nodes.length, "object")} · {countOf(view.edges.length, "connection")}
        </span>
      </div>

      {unreadable.length > 0 && (
        <p className="topo-unreadable">
          <EyeOffIcon size={14} />
          <span>
            Opscope can't read {listOf(unreadable.map((p) => pluralKind(kindName(p.resource))))} here, so they show
            as “Can't read”.
          </span>
        </p>
      )}

      {mode === "map" ? (
        <>
          <div ref={frame} className="topo-frame" style={{ height: height ?? filled }}>
            <TopologyGraph
              ref={graph}
              view={view}
              focus={view.focus}
              selected={shown}
              onSelect={setSelected}
              label={label}
              inset={shown ? PANEL_W : 0}
              fitKey={fitKey}
              empty={empty}
            />
            {shown && (
              <TopologyPanel
                view={view}
                id={shown}
                focus={view.focus}
                clusterId={clusterId}
                level={level}
                onSelect={(id) => {
                  setSelected(id);
                  graph.current?.showCard(id);
                }}
                onClose={close}
                expanded={expanded}
                onExpand={expand}
              />
            )}
          </div>
          <Legend view={view} />
        </>
      ) : (
        <TopologyList view={view} focus={view.focus} clusterId={clusterId} empty={empty} level={level} />
      )}
    </div>
  );
}

// Legend: what the lines and the marks on cards mean. Marks no card on this
// map carries are left out.
function Legend({ view }) {
  const has = (test) => view.nodes.some(test);
  const line = (style, optional) => (
    <svg width="30" height="10" aria-hidden="true" focusable="false">
      <path d="M 1 5 L 23 5" className={`topo-edge topo-edge-${style} ${optional ? "topo-edge-optional" : ""}`} />
      <path d="M 22 1.5 L 29 5 L 22 8.5 z" className="topo-legend-arrow" />
    </svg>
  );
  const styles = new Set(view.edges.map((e) => LINE_STYLES.find((s) => s.kinds.includes(e.kind))?.style));
  const marks = [
    has((n) => n.presence === "missing") && { Icon: MissingIcon, label: "Missing: referred to, doesn't exist" },
    has((n) => n.presence === "unknown") && { Icon: EyeOffIcon, label: "Can't read: Opscope can't read its kind" },
    has((n) => n.dangling?.length) && { Icon: UnlinkIcon, label: "Selects nothing" },
    has((n) => n.pods) && { Icon: PodsIcon, label: "Several pods of one workload" },
  ].filter(Boolean);
  if (view.nodes.length === 0) return null;
  return (
    <div className="topo-legend">
      <p className="topo-legend-intro">Arrows point to what an object needs.</p>
      <ul aria-label="Legend">
        {LINE_STYLES.filter((s) => styles.has(s.style)).map((s) => (
          <li key={s.style}>
            {line(s.style)}
            {s.label}
          </li>
        ))}
        {view.edges.some((e) => e.optional) && (
          <li>
            {line("dashed", true)}
            Optional
          </li>
        )}
        {marks.map((m) => (
          <li key={m.label}>
            <m.Icon size={14} className="topo-legend-icon" />
            {m.label}
          </li>
        ))}
        {view.focus && view.nodes.some((n) => n.id === view.focus) && (
          <li>
            <span className="topo-legend-here" aria-hidden="true" />
            This object
          </li>
        )}
      </ul>
    </div>
  );
}

// useFillHeight is how tall the map can be to fill the rest of the screen
// below where it starts, with the page scrolled to the top. It's measured
// when the map appears, once the fonts have loaded, and when the window
// changes size.
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
    // The page above can change height once the web fonts arrive.
    document.fonts?.ready.then(measure);
    window.addEventListener("resize", measure);
    return () => window.removeEventListener("resize", measure);
  }, [ref, enabled]);
  return height;
}

// listOf: "Secrets", "Secrets and Nodes", "Secrets, Nodes and Pods".
function listOf(words) {
  if (words.length < 2) return words.join("");
  return `${words.slice(0, -1).join(", ")} and ${words.at(-1)}`;
}
